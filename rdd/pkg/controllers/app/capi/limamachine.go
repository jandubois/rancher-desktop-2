// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	limav1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
	capiv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
	limavmcontrollers "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/lima/limavm/controllers"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/instance"
)

const (
	// guestClusterUIDLabel holds the UID of the guest cluster's kube-system
	// Namespace. The App's k3s cluster can be reset, so the label tells which
	// guest cluster a node VM and its input ConfigMap belong to.
	guestClusterUIDLabel = "capi.rancherdesktop.io/guest-cluster-uid"
	// machineNamespaceLabel holds the namespace of the node VM's LimaMachine.
	machineNamespaceLabel = "capi.rancherdesktop.io/machine-namespace"
	// machineNameLabel holds the name of the node VM's LimaMachine.
	machineNameLabel = "capi.rancherdesktop.io/machine-name"
	// machineUIDLabel holds the UID of the node VM's LimaMachine. rdd sets
	// it only on node VMs and their input ConfigMaps, so a LimaVM that has it
	// is a node VM.
	machineUIDLabel = "capi.rancherdesktop.io/machine-uid"

	// inputConfigMapSuffix gives the input ConfigMap of the LimaVM "<name>"
	// the name "<name>-source". The LimaVM controller stores its validated
	// copy of each template as "<vm>-template". So if the input ConfigMap had
	// the LimaVM's own name, a LimaMachine named "rd-template" would collide
	// with the App's copy, "rd-template", and fail with NameConflict.
	inputConfigMapSuffix = "-source"
)

// machineGVK is Cluster API's Machine. The provider reads Machines as
// unstructured objects so that it doesn't depend on Cluster API's Go module.
var machineGVK = schema.GroupVersionKind{Group: "cluster.x-k8s.io", Version: "v1beta2", Kind: "Machine"}

// controlPlaneError wraps an error from rdd's control plane. Reconcile returns
// such an error, so controller-runtime retries with backoff. An error from the
// guest cluster is only logged, and Reconcile requeues, unless the same pass
// also has a controlPlaneError. Then Reconcile returns both.
type controlPlaneError struct{ err error }

func (e *controlPlaneError) Error() string { return e.err.Error() }
func (e *controlPlaneError) Unwrap() error { return e.err }

// reconcileLimaMachines creates the node VM of every LimaMachine in the guest
// cluster and sets each LimaMachine's Ready condition. It reports whether any
// LimaMachine still waits for its Machine, its bootstrap data or its node VM.
func (r *Reconciler) reconcileLimaMachines(ctx context.Context, app *appv1alpha1.App) (bool, error) {
	var kubeSystem corev1.Namespace
	if err := r.guestClient.Get(ctx, client.ObjectKey{Name: metav1.NamespaceSystem}, &kubeSystem); err != nil {
		return false, fmt.Errorf("get the kube-system Namespace: %w", err)
	}
	var list capiv1alpha1.LimaMachineList
	if err := r.guestClient.List(ctx, &list); err != nil {
		return false, fmt.Errorf("list LimaMachines: %w", err)
	}
	inProgress := false
	var errs []error
	// An error with one LimaMachine doesn't stop the others.
	for i := range list.Items {
		machine := &list.Items[i]
		if !machine.DeletionTimestamp.IsZero() {
			continue
		}
		oldStatus := machine.Status.DeepCopy()
		reason, message, err := r.provisionLimaMachine(ctx, app, kubeSystem.UID, machine)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		switch reason {
		case capiv1alpha1.WaitingForMachineOwnerReason, capiv1alpha1.WaitingForBootstrapDataReason, capiv1alpha1.ProvisioningReason:
			inProgress = true
		}
		status := metav1.ConditionFalse
		if reason == capiv1alpha1.ReadyReason {
			status = metav1.ConditionTrue
		}
		apimeta.SetStatusCondition(&machine.Status.Conditions, metav1.Condition{
			Type:               capiv1alpha1.ReadyCondition,
			Status:             status,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: machine.Generation,
		})
		// SetStatusCondition reports only a change to the condition, but a
		// new probe can change the addresses while Ready stays True.
		if equality.Semantic.DeepEqual(oldStatus, &machine.Status) {
			continue
		}
		// As in reconcileLimaClusters, a LimaMachine that changed since the
		// List fails the update, and the next pass retries.
		if err := r.guestClient.Status().Update(ctx, machine); err != nil {
			errs = append(errs, fmt.Errorf("update LimaMachine %s/%s status: %w", machine.Namespace, machine.Name, err))
		}
	}
	return inProgress, errors.Join(errs...)
}

// provisionLimaMachine creates the node VM of machine once rdd has everything
// that the VM needs. Once the VM runs and its install script has succeeded,
// it writes machine's provider ID to the guest cluster, and it sets machine's
// addresses and provisioned status for the caller to write. It returns the
// reason and message for machine's Ready condition. Neither may quote the
// bootstrap data, which holds Rancher's registration token. Errors from rdd's
// control plane come back as a controlPlaneError.
func (r *Reconciler) provisionLimaMachine(ctx context.Context, app *appv1alpha1.App, clusterUID types.UID, machine *capiv1alpha1.LimaMachine) (reason, message string, err error) {
	log := logf.FromContext(ctx)
	owner := machineOwner(machine)
	if owner == nil {
		return capiv1alpha1.WaitingForMachineOwnerReason, "The LimaMachine does not have an owner Machine", nil
	}
	var capiMachine unstructured.Unstructured
	capiMachine.SetGroupVersionKind(machineGVK)
	if err := r.guestClient.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: owner.Name}, &capiMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return capiv1alpha1.WaitingForMachineOwnerReason, fmt.Sprintf("Machine %s does not exist", owner.Name), nil
		}
		return "", "", fmt.Errorf("get Machine %s/%s: %w", machine.Namespace, owner.Name, err)
	}

	if err := ValidateMachineName(r.LimaHome, machine.Name); err != nil {
		return capiv1alpha1.InvalidNameReason, err.Error(), nil
	}

	vmKey := client.ObjectKey{Namespace: appv1alpha1.AppNamespace, Name: machine.Name}
	inputKey := client.ObjectKey{Namespace: appv1alpha1.AppNamespace, Name: machine.Name + inputConfigMapSuffix}
	var vm limav1alpha1.LimaVM
	vmExists, err := r.getOwned(ctx, r.Client, vmKey, &vm, machine.UID)
	if err != nil {
		return "", "", err
	}
	switch vmExists {
	case conflicting:
		return capiv1alpha1.NameConflictReason, fmt.Sprintf("LimaVM %s belongs to something else", vmKey), nil
	case absent:
		// rdd's webhook rejects a LimaVM whose name is taken in any
		// namespace, because the name is also the Lima instance directory.
		// That rejection would reach only rdd's log, so check the name here
		// and report the conflict in the Ready condition.
		var vms limav1alpha1.LimaVMList
		if err := r.List(ctx, &vms); err != nil {
			return "", "", &controlPlaneError{fmt.Errorf("list LimaVMs: %w", err)}
		}
		for _, other := range vms.Items {
			if other.Name == vmKey.Name && other.Namespace != vmKey.Namespace {
				return capiv1alpha1.NameConflictReason, fmt.Sprintf("LimaVM %s/%s belongs to something else", other.Namespace, other.Name), nil
			}
		}
		return r.createNodeVM(ctx, app, clusterUID, machine, &capiMachine, vmKey, inputKey)
	case owned:
	}

	// The LimaVM has its own copy of the template, so the input ConfigMap is
	// no longer needed. Neither is the bootstrap data. So once the VM exists,
	// rdd stops reading the registration token, and a later change to the
	// Machine's version or its Secret doesn't change the Ready condition.
	var input corev1.ConfigMap
	inputExists, err := r.getOwned(ctx, r.Client, inputKey, &input, machine.UID)
	if err != nil {
		return "", "", err
	}
	if inputExists == owned {
		if err := client.IgnoreNotFound(r.Delete(ctx, &input)); err != nil {
			return "", "", &controlPlaneError{fmt.Errorf("delete ConfigMap %s: %w", inputKey, err)}
		}
	}

	// Lima's YAML errors quote the template lines near the error, and the
	// template holds the install script with Rancher's registration token.
	// So when the LimaVM fails, the Ready condition copies the LimaVM's
	// reason but not its message, and names the rdd command that shows it.
	for _, failure := range []struct{ conditionType, reason, verb string }{
		{limavmcontrollers.ConditionCreated, limavmcontrollers.ReasonCreateFailed, "create"},
		{limavmcontrollers.ConditionRunning, limavmcontrollers.ReasonStartFailed, "start"},
	} {
		condition := apimeta.FindStatusCondition(vm.Status.Conditions, failure.conditionType)
		if condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == failure.reason {
			rdd := "rdd"
			if suffix := instance.Suffix(); suffix != instance.DefaultSuffix {
				rdd += " --instance=" + suffix
			}
			return failure.reason, fmt.Sprintf("rdd could not %s the node VM; run \"%s ctl get limavm --namespace %s %s --output yaml\" to see why",
				failure.verb, rdd, vmKey.Namespace, vmKey.Name), nil
		}
	}
	const provisioning = "The node VM is being provisioned"
	if !apimeta.IsStatusConditionTrue(vm.Status.Conditions, limavmcontrollers.ConditionRunning) {
		return capiv1alpha1.ProvisioningReason, provisioning, nil
	}
	// r.probes keeps the last probe of the current boot that found the VM's
	// address. A restart can change the address, so a probe from an earlier
	// boot doesn't count. Lima stops waiting for its boot scripts after a
	// while, so the install script can still succeed after the VM runs.
	// Until a probe finds the marker, probe again on every pass. A failed
	// probe leaves the last result of the boot in place.
	probe, ok := r.probes[machine.UID]
	ok = ok && probe.restartCount == vm.Status.RestartCount
	if !ok || !probe.status.bootstrapped {
		status, err := r.probeNode(ctx, vm.Name)
		switch {
		case err != nil:
			// The error can quote the VM's Lima template, which holds the
			// registration token, so it stays out of the message.
			log.V(1).Info("Cannot probe the node VM", "limaVM", vmKey, "err", err)
		case status.address != "":
			probe = probeRecord{restartCount: vm.Status.RestartCount, status: status}
			if r.probes == nil {
				r.probes = make(map[types.UID]probeRecord)
			}
			r.probes[machine.UID] = probe
			ok = true
		}
		if !ok {
			return capiv1alpha1.ProvisioningReason, provisioning, nil
		}
	}
	if !probe.status.bootstrapped {
		return capiv1alpha1.BootstrapFailedReason, "The install script has not succeeded in the node VM yet; a failed script runs again when the VM restarts", nil
	}

	// Update replaces machine with the server's copy, so it comes before
	// the changes to machine's status.
	hostname := "lima-" + machine.Name
	if providerID := "k3s://" + hostname; machine.Spec.ProviderID != providerID {
		machine.Spec.ProviderID = providerID
		if err := r.guestClient.Update(ctx, machine); err != nil {
			return "", "", fmt.Errorf("update LimaMachine %s/%s: %w", machine.Namespace, machine.Name, err)
		}
	}
	machine.Status.Addresses = capiv1alpha1.MachineAddresses{
		{Type: capiv1alpha1.MachineInternalIP, Address: probe.status.address},
		{Type: capiv1alpha1.MachineHostName, Address: hostname},
	}
	machine.Status.Initialization.Provisioned = ptr.To(true)
	return capiv1alpha1.ReadyReason, "", nil
}

// createNodeVM checks the owner Machine's version and bootstrap data, then
// creates the input ConfigMap inputKey and the LimaVM vmKey for machine. It
// returns the reason and message for machine's Ready condition, like
// provisionLimaMachine.
func (r *Reconciler) createNodeVM(ctx context.Context, app *appv1alpha1.App, clusterUID types.UID, machine *capiv1alpha1.LimaMachine, capiMachine *unstructured.Unstructured, vmKey, inputKey client.ObjectKey) (reason, message string, err error) {
	version, _, _ := unstructured.NestedString(capiMachine.Object, "spec", "version")
	if !strings.Contains(version, "+k3s") {
		return capiv1alpha1.UnsupportedDistributionReason,
			fmt.Sprintf("Machine version %q is not a K3s version; only K3s clusters are supported", version), nil
	}

	secretName, _, _ := unstructured.NestedString(capiMachine.Object, "spec", "bootstrap", "dataSecretName")
	if secretName == "" {
		return capiv1alpha1.WaitingForBootstrapDataReason, "The Machine does not name its bootstrap data Secret yet", nil
	}
	var secret corev1.Secret
	if err := r.guestClient.Get(ctx, client.ObjectKey{Namespace: machine.Namespace, Name: secretName}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return capiv1alpha1.WaitingForBootstrapDataReason, fmt.Sprintf("Secret %s does not exist", secretName), nil
		}
		return "", "", fmt.Errorf("get Secret %s/%s: %w", machine.Namespace, secretName, err)
	}
	data, ok := secret.Data["value"]
	if !ok {
		return capiv1alpha1.UnsupportedBootstrapDataReason, fmt.Sprintf("Secret %s does not have a \"value\" key", secretName), nil
	}
	script, err := InstallScript(data)
	if err != nil {
		return capiv1alpha1.UnsupportedBootstrapDataReason, err.Error(), nil
	}

	// The cache can still hold an input ConfigMap that an earlier pass
	// deleted moments ago after it failed to create the LimaVM, so read the
	// ConfigMap from the API server.
	var input corev1.ConfigMap
	inputExists, err := r.getOwned(ctx, r.apiReader, inputKey, &input, machine.UID)
	if err != nil {
		return "", "", err
	}
	labels := nodeVMLabels(clusterUID, machine)
	switch inputExists {
	case conflicting:
		return capiv1alpha1.NameConflictReason, fmt.Sprintf("ConfigMap %s belongs to something else", inputKey), nil
	case absent:
		template, err := LimaTemplate(machine.Spec, script)
		if err != nil {
			return "", "", fmt.Errorf("render the Lima template of LimaMachine %s/%s: %w", machine.Namespace, machine.Name, err)
		}
		input = corev1.ConfigMap{
			Name:      inputKey.Name,
			Namespace: inputKey.Namespace,
			Labels:    labels,
			Data:      map[string]string{limav1alpha1.TemplateConfigMapKey: string(template)},
		}
		if err := r.createOwnedByApp(ctx, app, &input); err != nil {
			return "", "", err
		}
	case owned:
	}

	vm := limav1alpha1.LimaVM{
		Name:       vmKey.Name,
		Namespace:  vmKey.Namespace,
		Labels:     labels,
		Finalizers: []string{base.OwnedFinalizerFor("LimaMachine")},
		Spec: limav1alpha1.LimaVMSpec{
			TemplateRef: limav1alpha1.TemplateReference{Name: inputKey.Name, Namespace: inputKey.Namespace},
			// The pass runs only while the App runs with Kubernetes ready.
			Running: true,
		},
	}
	if err := r.createOwnedByApp(ctx, app, &vm); err != nil {
		// Without the input ConfigMap, the next pass renders the template
		// again, so a corrected spec takes effect.
		if deleteErr := client.IgnoreNotFound(r.Delete(ctx, &input)); deleteErr != nil {
			return "", "", errors.Join(err, &controlPlaneError{fmt.Errorf("delete ConfigMap %s: %w", inputKey, deleteErr)})
		}
		return "", "", err
	}
	return capiv1alpha1.ProvisioningReason, "Creating the node VM", nil
}

// machineOwner returns machine's owner reference to a Cluster API Machine, or
// nil when it has none.
func machineOwner(machine *capiv1alpha1.LimaMachine) *metav1.OwnerReference {
	for i, ref := range machine.OwnerReferences {
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err == nil && gv.Group == machineGVK.Group && ref.Kind == machineGVK.Kind {
			return &machine.OwnerReferences[i]
		}
	}
	return nil
}

// nodeVMLabels returns the labels of the node VM of machine and of its input
// ConfigMap.
func nodeVMLabels(clusterUID types.UID, machine *capiv1alpha1.LimaMachine) map[string]string {
	return map[string]string{
		guestClusterUIDLabel:  string(clusterUID),
		machineNamespaceLabel: machine.Namespace,
		machineNameLabel:      machine.Name,
		machineUIDLabel:       string(machine.UID),
	}
}

// ownership says whether an object in rdd's control plane exists, and
// whether it belongs to a LimaMachine.
type ownership int

const (
	absent ownership = iota
	owned
	conflicting
)

// getOwned reads key into obj from rdd's control plane through reader, and
// reports whether it is absent, labelled for the LimaMachine with machineUID,
// or anything else.
func (r *Reconciler) getOwned(ctx context.Context, reader client.Reader, key client.ObjectKey, obj client.Object, machineUID types.UID) (ownership, error) {
	if err := reader.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return absent, nil
		}
		return absent, &controlPlaneError{fmt.Errorf("get %s: %w", key, err)}
	}
	if obj.GetLabels()[machineUIDLabel] != string(machineUID) {
		return conflicting, nil
	}
	return owned, nil
}

// createOwnedByApp creates obj in rdd's control plane with a non-controller
// owner reference to the App, so that deleting the App deletes obj too.
func (r *Reconciler) createOwnedByApp(ctx context.Context, app *appv1alpha1.App, obj client.Object) error {
	if err := controllerutil.SetOwnerReference(app, obj, r.Scheme()); err != nil {
		return &controlPlaneError{fmt.Errorf("set owner reference on %s: %w", obj.GetName(), err)}
	}
	if err := r.Create(ctx, obj); err != nil {
		return &controlPlaneError{fmt.Errorf("create %s: %w", client.ObjectKeyFromObject(obj), err)}
	}
	return nil
}
