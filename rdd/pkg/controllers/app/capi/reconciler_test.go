// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/lima-vm/lima/v2/pkg/limayaml"
	"gotest.tools/v3/assert"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	limav1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
	guestcapi "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi"
	capiv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
	limavmcontrollers "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/lima/limavm/controllers"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/crds"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/testapiserver"
)

// newApp returns a running App with Kubernetes enabled, and with the Running
// and KubernetesReady conditions set to the given statuses.
func newApp(running, kubernetesReady metav1.ConditionStatus) *appv1alpha1.App {
	return &appv1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{Name: appName, Generation: 1},
		Spec: appv1alpha1.AppSpec{
			Running:    true,
			Kubernetes: appv1alpha1.KubernetesSpec{Enabled: true},
		},
		Status: appv1alpha1.AppStatus{
			Conditions: []metav1.Condition{
				{Type: appv1alpha1.AppConditionRunning, Status: running, Reason: "Test"},
				{Type: appv1alpha1.AppConditionKubernetesReady, Status: kubernetesReady, Reason: "Test"},
			},
		},
	}
}

func TestAppPredicate(t *testing.T) {
	base := newApp(metav1.ConditionTrue, metav1.ConditionTrue)
	base.Status.Conditions = append(base.Status.Conditions, metav1.Condition{
		Type: "Other", Status: metav1.ConditionTrue, Reason: "Test",
	})
	tests := []struct {
		name   string
		change func(app *appv1alpha1.App)
		want   bool
	}{
		{"generation bump", func(app *appv1alpha1.App) { app.Generation++ }, true},
		{"Running flip", func(app *appv1alpha1.App) { app.Status.Conditions[0].Status = metav1.ConditionFalse }, true},
		{"KubernetesReady flip", func(app *appv1alpha1.App) { app.Status.Conditions[1].Status = metav1.ConditionFalse }, true},
		{"other condition flip", func(app *appv1alpha1.App) { app.Status.Conditions[2].Status = metav1.ConditionFalse }, false},
		{"transition time only", func(app *appv1alpha1.App) {
			app.Status.Conditions[0].LastTransitionTime = metav1.NewTime(time.Unix(1000, 0))
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated := base.DeepCopy()
			tt.change(updated)
			got := appPredicate().Update(event.UpdateEvent{ObjectOld: base, ObjectNew: updated})
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestReconcile(t *testing.T) {
	env := testapiserver.Start(t)
	ctx := t.Context()
	clientset, err := apiextensionsclientset.NewForConfig(env.Config)
	assert.NilError(t, err)
	guestCRDs := clientset.ApiextensionsV1().CustomResourceDefinitions()

	kubeConfig := guestKubeConfig(t, env)

	k3sConfigPath := filepath.Join(t.TempDir(), "k3s.yaml")
	unreachable := `apiVersion: v1
kind: Config
clusters:
- name: k3s
  cluster:
    server: https://127.0.0.1:1
contexts:
- name: k3s
  context:
    cluster: k3s
    user: k3s
current-context: k3s
users:
- name: k3s
  user:
    token: unused
`
	assert.NilError(t, os.WriteFile(k3sConfigPath, []byte(unreachable), 0o600))

	scheme := k8sruntime.NewScheme()
	assert.NilError(t, appv1alpha1.AddToScheme(scheme))
	assert.NilError(t, corev1.AddToScheme(scheme))
	assert.NilError(t, limav1alpha1.AddToScheme(scheme))
	app := newApp(metav1.ConditionTrue, metav1.ConditionFalse)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app).Build()
	r := &Reconciler{Client: c, apiReader: c, K3sConfigPath: k3sConfigPath}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName}}

	ourCRDs, err := crds.Decode(guestcapi.CRDs)
	assert.NilError(t, err)

	// a. Kubernetes not ready: nothing to do until the App changes.
	result, err := r.Reconcile(ctx, req)
	assert.NilError(t, err)
	assert.Equal(t, result, ctrl.Result{})

	// b. The guest cannot be reached: retry soon, without an error.
	setConditionsTrue(app)
	assert.NilError(t, c.Status().Update(ctx, app))
	result, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	assert.Equal(t, result, ctrl.Result{RequeueAfter: requeueInProgress})

	// c. A reachable guest without Cluster API stays untouched. Reaching it
	// at all shows the client was rebuilt from the changed k3s.yaml. The
	// node VM and input ConfigMap of an earlier guest cluster still go, even
	// with an owned finalizer, and the current guest cluster's node VM stays.
	guest, err := client.New(env.Config, client.Options{Scheme: guestScheme})
	assert.NilError(t, err)
	var kubeSystem corev1.Namespace
	assert.NilError(t, guest.Get(ctx, client.ObjectKey{Name: metav1.NamespaceSystem}, &kubeSystem))
	oldLabels := map[string]string{guestClusterUIDLabel: "old-cluster", machineUIDLabel: "old-machine"}
	ownedFinalizer := []string{base.OwnedFinalizerFor("LimaMachine")}
	stale := []client.Object{
		&limav1alpha1.LimaVM{Namespace: appv1alpha1.AppNamespace, Name: "stale", Labels: oldLabels, Finalizers: ownedFinalizer},
		&corev1.ConfigMap{Namespace: appv1alpha1.AppNamespace, Name: "stale-source", Labels: oldLabels, Finalizers: ownedFinalizer},
	}
	current := &limav1alpha1.LimaVM{Namespace: appv1alpha1.AppNamespace, Name: "current", Labels: map[string]string{
		guestClusterUIDLabel: string(kubeSystem.UID), machineUIDLabel: "current-machine",
	}}
	for _, obj := range append(stale, current) {
		assert.NilError(t, c.Create(ctx, obj))
	}
	assert.NilError(t, os.WriteFile(k3sConfigPath, kubeConfig, 0o600))
	result, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	assert.Equal(t, result, ctrl.Result{RequeueAfter: requeueIdle})
	for _, crd := range ourCRDs {
		_, err := guestCRDs.Get(ctx, crd.Name, metav1.GetOptions{})
		assert.Assert(t, apierrors.IsNotFound(err), "%s: %v", crd.Name, err)
	}
	for _, obj := range stale {
		err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		assert.Assert(t, apierrors.IsNotFound(err), "%s was not removed: %v", obj.GetName(), err)
	}
	assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(current), current), "the current guest cluster's LimaVM")

	// d. Once Cluster API's Machine CRD exists, our CRDs are installed.
	_, err = guestCRDs.Create(ctx, machineCRD(), metav1.CreateOptions{})
	assert.NilError(t, err)
	result, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	assert.Equal(t, result, ctrl.Result{RequeueAfter: requeueIdle})
	for _, crd := range ourCRDs {
		stored, err := guestCRDs.Get(ctx, crd.Name, metav1.GetOptions{})
		assert.NilError(t, err)
		assert.Equal(t, stored.Labels["cluster.x-k8s.io/v1beta2"], "v1alpha1", crd.Name)
		assert.Assert(t, established(stored), "%s is not established", crd.Name)
	}

	// e. A LimaCluster is marked provisioned and ready.
	cluster := &capiv1alpha1.LimaCluster{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test"}}
	assert.NilError(t, guest.Create(ctx, cluster))
	result, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	assert.Equal(t, result, ctrl.Result{RequeueAfter: requeueIdle})
	assert.NilError(t, guest.Get(ctx, client.ObjectKeyFromObject(cluster), cluster))
	assert.Assert(t, cluster.Status.Initialization.Provisioned != nil && *cluster.Status.Initialization.Provisioned)
	ready := apimeta.FindStatusCondition(cluster.Status.Conditions, capiv1alpha1.ReadyCondition)
	assert.Assert(t, ready != nil)
	assert.Equal(t, ready.Status, metav1.ConditionTrue)
	assert.Equal(t, ready.Reason, capiv1alpha1.ReadyReason)
	assert.Equal(t, ready.ObservedGeneration, cluster.Generation)
}

func TestReconcileLimaMachines(t *testing.T) {
	env := testapiserver.Start(t)
	ctx := logf.IntoContext(t.Context(), testr.New(t))
	clientset, err := apiextensionsclientset.NewForConfig(env.Config)
	assert.NilError(t, err)
	guestCRDs := clientset.ApiextensionsV1().CustomResourceDefinitions()
	// installCRD creates crd in the guest cluster and waits until the guest
	// cluster serves its kind.
	installCRD := func(crd *apiextensionsv1.CustomResourceDefinition) {
		t.Helper()
		_, err := guestCRDs.Create(ctx, crd, metav1.CreateOptions{})
		assert.NilError(t, err)
		assert.NilError(t, crds.WaitEstablished(ctx, guestCRDs, crd.Name, time.Minute))
	}
	rkeControlPlane := schema.GroupVersionKind{Group: "rke.cattle.io", Version: "v1", Kind: "RKEControlPlane"}
	// k3sControlPlane stands in for a control plane provider that follows
	// Cluster API's contract and sets spec.version.
	k3sControlPlane := schema.GroupVersionKind{Group: "controlplane.cluster.x-k8s.io", Version: "v1beta2", Kind: "KThreesControlPlane"}
	installCRD(machineCRD())
	installCRD(standInCRD(rkeControlPlane.Group, rkeControlPlane.Version, rkeControlPlane.Kind, "rkecontrolplanes"))
	installCRD(standInCRD(k3sControlPlane.Group, k3sControlPlane.Version, k3sControlPlane.Kind, "kthreescontrolplanes"))
	k3sConfigPath := filepath.Join(t.TempDir(), "k3s.yaml")
	assert.NilError(t, os.WriteFile(k3sConfigPath, guestKubeConfig(t, env), 0o600))

	scheme := k8sruntime.NewScheme()
	assert.NilError(t, appv1alpha1.AddToScheme(scheme))
	assert.NilError(t, corev1.AddToScheme(scheme))
	assert.NilError(t, limav1alpha1.AddToScheme(scheme))
	app := newApp(metav1.ConditionTrue, metav1.ConditionTrue)
	taken := &limav1alpha1.LimaVM{Name: "taken", Namespace: appv1alpha1.AppNamespace}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app, taken).WithStatusSubresource(app).Build()
	// A short LIMA_HOME keeps the socket path limit out of the way.
	r := &Reconciler{Client: c, apiReader: c, K3sConfigPath: k3sConfigPath, LimaHome: "/l"}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName}}

	// The first pass installs our CRDs.
	_, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	guest, err := client.New(env.Config, client.Options{Scheme: guestScheme})
	assert.NilError(t, err)

	script := []byte("#!/bin/sh\necho " + secret + "\n")
	assert.NilError(t, guest.Create(ctx, &corev1.Secret{
		Namespace: "default", Name: "bootstrap",
		Data: map[string][]byte{"value": cloudConfigFor("", gzipBase64(t, script), "", "")},
	}))
	assert.NilError(t, guest.Create(ctx, &corev1.Secret{
		Namespace: "default", Name: "unsupported",
		Data: map[string][]byte{"value": []byte("#cloud-config\n{}\n")},
	}))
	// createCluster creates a Cluster API Cluster name whose controlPlaneRef
	// names a control plane object of kind controlPlane, and creates that
	// object with the given spec. A zero controlPlane leaves out the
	// controlPlaneRef, and a nil spec leaves out the control plane object.
	createCluster := func(name string, controlPlane schema.GroupVersionKind, spec map[string]any) {
		t.Helper()
		cluster := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
		if !controlPlane.Empty() {
			cluster.Object["spec"] = map[string]any{"controlPlaneRef": map[string]any{
				"apiGroup": controlPlane.Group, "kind": controlPlane.Kind, "name": name,
			}}
		}
		cluster.SetGroupVersionKind(clusterGVK)
		cluster.SetNamespace("default")
		cluster.SetName(name)
		assert.NilError(t, guest.Create(ctx, cluster))
		if spec == nil {
			return
		}
		object := &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
		object.SetGroupVersionKind(controlPlane)
		object.SetNamespace("default")
		object.SetName(name)
		assert.NilError(t, guest.Create(ctx, object))
	}
	// createMachine creates a LimaMachine. Unless clusterName is empty, it
	// also creates a Machine in that cluster with the given bootstrap Secret
	// name, and makes that Machine the LimaMachine's owner. Like Rancher's
	// Machines, the Machine doesn't have a spec.version.
	createMachine := func(name, clusterName, secretName string) {
		t.Helper()
		limaMachine := &capiv1alpha1.LimaMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name}}
		if clusterName != "" {
			machine := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{"clusterName": clusterName, "bootstrap": map[string]any{"dataSecretName": secretName}},
			}}
			machine.SetGroupVersionKind(machineGVK)
			machine.SetNamespace("default")
			machine.SetName(name)
			assert.NilError(t, guest.Create(ctx, machine))
			limaMachine.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: machineGVK.GroupVersion().String(), Kind: machineGVK.Kind, Name: name, UID: machine.GetUID(),
			}}
		}
		assert.NilError(t, guest.Create(ctx, limaMachine))
	}
	// assertReady checks that LimaMachine name has a False Ready condition
	// with the given reason, and returns that condition.
	assertReady := func(name, reason string) *metav1.Condition {
		t.Helper()
		var limaMachine capiv1alpha1.LimaMachine
		assert.NilError(t, guest.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, &limaMachine))
		ready := apimeta.FindStatusCondition(limaMachine.Status.Conditions, capiv1alpha1.ReadyCondition)
		assert.Assert(t, ready != nil, name)
		assert.Equal(t, ready.Status, metav1.ConditionFalse, name)
		assert.Equal(t, ready.Reason, reason, name)
		assert.Equal(t, ready.ObservedGeneration, limaMachine.Generation, name)
		assert.Assert(t, !strings.Contains(ready.Message, secret), "%s: message leaks the script", name)
		return ready
	}

	// Until the guest cluster serves the Cluster kind, a LimaMachine waits
	// for its control plane.
	createMachine("nocluster", "missing", "bootstrap")
	_, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	ready := assertReady("nocluster", capiv1alpha1.WaitingForControlPlaneReason)
	assert.Equal(t, ready.Message, "The guest cluster does not serve Cluster.cluster.x-k8s.io yet")

	installCRD(standInCRD(clusterGVK.Group, clusterGVK.Version, clusterGVK.Kind, "clusters"))
	createCluster("k3s", rkeControlPlane, map[string]any{"kubernetesVersion": "v1.36.6+k3s1"})
	createCluster("rke2", rkeControlPlane, map[string]any{"kubernetesVersion": "v1.36.6+rke2r1"})
	createCluster("nocontrolplane", rkeControlPlane, nil)
	createCluster("noref", schema.GroupVersionKind{}, nil)
	createCluster("unserved", schema.GroupVersionKind{Group: "controlplane.example.com", Version: "v1", Kind: "UnservedControlPlane"}, nil)
	createCluster("noversion", rkeControlPlane, map[string]any{})
	createCluster("k3sprovider", k3sControlPlane, map[string]any{"version": "v1.36.6+k3s1"})

	longName := strings.Repeat("a", 60)
	createMachine("happy", "k3s", "bootstrap")
	createMachine("orphan", "", "")
	createMachine("rke2", "rke2", "bootstrap")
	createMachine("nocontrolplane", "nocontrolplane", "bootstrap")
	createMachine("noref", "noref", "bootstrap")
	createMachine("unserved", "unserved", "bootstrap")
	createMachine("noversion", "noversion", "bootstrap")
	// k3sprovider gets unsupported bootstrap data, so it stops right after it
	// passes the version check, and rdd doesn't create a LimaVM for it.
	createMachine("k3sprovider", "k3sprovider", "unsupported")
	createMachine("nosecret", "k3s", "")
	createMachine("badsecret", "k3s", "unsupported")
	createMachine(longName, "k3s", "bootstrap")
	createMachine("taken", "k3s", "bootstrap")

	result, err := r.Reconcile(ctx, req)
	assert.NilError(t, err)
	assert.Equal(t, result, ctrl.Result{RequeueAfter: requeueInProgress})
	assertReady("happy", capiv1alpha1.ProvisioningReason)
	assertReady("orphan", capiv1alpha1.WaitingForMachineOwnerReason)
	assertReady("rke2", capiv1alpha1.UnsupportedDistributionReason)
	// The first pass looked up the cluster.x-k8s.io group while that group
	// had only the Machine kind. The guest client still finds the Cluster
	// kind that was added to the group afterwards.
	ready = assertReady("nocluster", capiv1alpha1.WaitingForControlPlaneReason)
	assert.Equal(t, ready.Message, "Cluster missing does not exist")
	assertReady("nocontrolplane", capiv1alpha1.WaitingForControlPlaneReason)
	assertReady("noref", capiv1alpha1.WaitingForControlPlaneReason)
	ready = assertReady("unserved", capiv1alpha1.WaitingForControlPlaneReason)
	assert.Equal(t, ready.Message, "The guest cluster does not serve UnservedControlPlane.controlplane.example.com yet")
	assertReady("noversion", capiv1alpha1.WaitingForControlPlaneReason)
	assertReady("k3sprovider", capiv1alpha1.UnsupportedBootstrapDataReason)
	assertReady("nosecret", capiv1alpha1.WaitingForBootstrapDataReason)
	assertReady("badsecret", capiv1alpha1.UnsupportedBootstrapDataReason)
	assertReady(longName, capiv1alpha1.InvalidNameReason)
	assertReady("taken", capiv1alpha1.NameConflictReason)

	// Only a LimaMachine with an owner Machine gets the finalizer.
	for name, want := range map[string]bool{"happy": true, "orphan": false} {
		var limaMachine capiv1alpha1.LimaMachine
		assert.NilError(t, guest.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, &limaMachine))
		assert.Equal(t, controllerutil.ContainsFinalizer(&limaMachine, capiv1alpha1.MachineFinalizer), want,
			"%s: has the LimaMachine finalizer", name)
	}

	var happy capiv1alpha1.LimaMachine
	assert.NilError(t, guest.Get(ctx, client.ObjectKey{Namespace: "default", Name: "happy"}, &happy))
	var kubeSystem corev1.Namespace
	assert.NilError(t, guest.Get(ctx, client.ObjectKey{Name: metav1.NamespaceSystem}, &kubeSystem))
	wantLabels := map[string]string{
		guestClusterUIDLabel:  string(kubeSystem.UID),
		machineNamespaceLabel: "default",
		machineNameLabel:      "happy",
		machineUIDLabel:       string(happy.UID),
	}
	// assertOwnedByApp checks that the App owns obj, but doesn't control it.
	assertOwnedByApp := func(obj client.Object) {
		t.Helper()
		refs := obj.GetOwnerReferences()
		assert.Equal(t, len(refs), 1, obj.GetName())
		assert.Equal(t, refs[0].Kind, appv1alpha1.AppKind)
		assert.Equal(t, refs[0].Name, appName)
		assert.Assert(t, refs[0].Controller == nil || !*refs[0].Controller, "%s: the App is its controller", obj.GetName())
	}

	var vms limav1alpha1.LimaVMList
	assert.NilError(t, c.List(ctx, &vms))
	assert.Equal(t, len(vms.Items), 2, "only happy and the pre-existing taken have LimaVMs")
	var configMaps corev1.ConfigMapList
	assert.NilError(t, c.List(ctx, &configMaps))
	assert.Equal(t, len(configMaps.Items), 1, "only happy has an input ConfigMap")
	input := &configMaps.Items[0]
	assert.Equal(t, input.Namespace, appv1alpha1.AppNamespace)
	assert.Equal(t, input.Name, "happy-source")
	assert.DeepEqual(t, input.Labels, wantLabels)
	assertOwnedByApp(input)
	t.Setenv("LIMA_HOME", t.TempDir())
	_, err = limayaml.Load(ctx, []byte(input.Data[limav1alpha1.TemplateConfigMapKey]), "")
	assert.NilError(t, err)

	vm := &limav1alpha1.LimaVM{}
	assert.NilError(t, c.Get(ctx, client.ObjectKey{Namespace: appv1alpha1.AppNamespace, Name: "happy"}, vm))
	assert.DeepEqual(t, vm.Labels, wantLabels)
	assert.DeepEqual(t, vm.Finalizers, []string{base.OwnedFinalizerFor("LimaMachine")})
	assertOwnedByApp(vm)
	assert.Equal(t, vm.Spec.TemplateRef, limav1alpha1.TemplateReference{Name: "happy-source", Namespace: appv1alpha1.AppNamespace})
	assert.Assert(t, vm.Spec.Running)

	// Once the LimaVM exists, the next pass deletes the input ConfigMap and
	// stops reading the bootstrap data, so deleting the Secret leaves Ready
	// unchanged.
	assert.NilError(t, guest.Delete(ctx, &corev1.Secret{Namespace: "default", Name: "bootstrap"}))
	_, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	assertReady("happy", capiv1alpha1.ProvisioningReason)
	err = c.Get(ctx, client.ObjectKeyFromObject(input), &corev1.ConfigMap{})
	assert.Assert(t, apierrors.IsNotFound(err), "input ConfigMap: %v", err)

	// A LimaVM failure shows in the LimaMachine's Ready condition, but the
	// LimaVM's message does not, because it can quote the template.
	for _, failure := range []struct{ conditionType, reason string }{
		{limavmcontrollers.ConditionCreated, limavmcontrollers.ReasonCreateFailed},
		{limavmcontrollers.ConditionRunning, limavmcontrollers.ReasonStartFailed},
	} {
		assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(vm), vm))
		vm.Status.Conditions = []metav1.Condition{{
			Type: failure.conditionType, Status: metav1.ConditionFalse,
			Reason: failure.reason, Message: secret,
		}}
		assert.NilError(t, c.Update(ctx, vm))
		_, err = r.Reconcile(ctx, req)
		assert.NilError(t, err)
		ready := assertReady("happy", failure.reason)
		assert.Assert(t, strings.Contains(ready.Message, "--namespace "+appv1alpha1.AppNamespace+" happy "), ready.Message)
	}

	// Once the LimaVM runs, the probe decides Ready. A probe that finds the
	// marker holds until the VM restarts.
	var probes int
	var probeStatus nodeStatus
	var probeErr error
	r.probeNode = func(_ context.Context, vmName string) (nodeStatus, error) {
		assert.Equal(t, vmName, "happy")
		probes++
		return probeStatus, probeErr
	}
	// boot marks the LimaVM running for the restartCount-th time.
	boot := func(restartCount int32) {
		t.Helper()
		assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(vm), vm))
		vm.Status.Conditions = []metav1.Condition{
			{Type: limavmcontrollers.ConditionCreated, Status: metav1.ConditionTrue, Reason: "Created"},
			{Type: limavmcontrollers.ConditionRunning, Status: metav1.ConditionTrue, Reason: "Running"},
		}
		vm.Status.RestartCount = restartCount
		assert.NilError(t, c.Update(ctx, vm))
	}
	reconcileHappy := func() {
		t.Helper()
		_, err := r.Reconcile(ctx, req)
		assert.NilError(t, err)
		assert.NilError(t, guest.Get(ctx, client.ObjectKey{Namespace: "default", Name: "happy"}, &happy))
	}
	assertAddress := func(address string) {
		t.Helper()
		assert.DeepEqual(t, happy.Status.Addresses, capiv1alpha1.MachineAddresses{
			{Type: capiv1alpha1.MachineInternalIP, Address: address},
			{Type: capiv1alpha1.MachineHostName, Address: "lima-happy"},
		})
	}

	boot(1)
	probeStatus = nodeStatus{address: "192.168.105.3", bootstrapped: true}
	reconcileHappy()
	ready = apimeta.FindStatusCondition(happy.Status.Conditions, capiv1alpha1.ReadyCondition)
	assert.Assert(t, ready != nil)
	assert.Equal(t, ready.Status, metav1.ConditionTrue)
	assert.Equal(t, ready.Reason, capiv1alpha1.ReadyReason)
	assert.Equal(t, happy.Spec.ProviderID, "k3s://lima-happy")
	assertAddress("192.168.105.3")
	assert.Assert(t, ptr.Deref(happy.Status.Initialization.Provisioned, false))
	assert.Equal(t, probes, 1)

	reconcileHappy()
	assert.Equal(t, probes, 1, "the same boot is probed again")

	boot(2)
	probeStatus = nodeStatus{address: "192.168.105.4", bootstrapped: true}
	reconcileHappy()
	assert.Equal(t, probes, 2, "a restart is not probed")
	assertAddress("192.168.105.4")

	// A failed probe or a failed install script turns Ready False, but
	// the LimaMachine stays provisioned.
	boot(3)
	probeErr = errors.New(secret)
	reconcileHappy()
	assertReady("happy", capiv1alpha1.ProvisioningReason)
	assert.Assert(t, ptr.Deref(happy.Status.Initialization.Provisioned, false))

	probeStatus, probeErr = nodeStatus{address: "192.168.105.5"}, nil
	reconcileHappy()
	assertReady("happy", capiv1alpha1.BootstrapFailedReason)
	assert.Assert(t, ptr.Deref(happy.Status.Initialization.Provisioned, false))

	// A failed probe keeps the last result of the boot.
	probeErr = errors.New(secret)
	reconcileHappy()
	assertReady("happy", capiv1alpha1.BootstrapFailedReason)
	probeErr = nil

	// Lima can report the VM running before a slow install script finishes,
	// so the same boot is probed again until the marker shows up.
	probeStatus = nodeStatus{address: "192.168.105.6", bootstrapped: true}
	reconcileHappy()
	ready = apimeta.FindStatusCondition(happy.Status.Conditions, capiv1alpha1.ReadyCondition)
	assert.Equal(t, ready.Reason, capiv1alpha1.ReadyReason)
	assertAddress("192.168.105.6")

	// The node VMs run only while the App runs with Kubernetes enabled. The
	// unlabelled "taken" LimaVM is not a node VM, so it stays stopped.
	setApp := func(running, kubernetes bool, conditions metav1.ConditionStatus) {
		t.Helper()
		assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(app), app))
		app.Spec.Running = running
		app.Spec.Kubernetes.Enabled = kubernetes
		assert.NilError(t, c.Update(ctx, app))
		assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(app), app))
		for i := range app.Status.Conditions {
			app.Status.Conditions[i].Status = conditions
		}
		assert.NilError(t, c.Status().Update(ctx, app))
		_, err := r.Reconcile(ctx, req)
		assert.NilError(t, err)
	}
	assertRunning := func(step string, want bool) {
		t.Helper()
		assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(vm), vm))
		assert.Equal(t, vm.Spec.Running, want, "%s: happy's LimaVM spec.running", step)
		assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(taken), taken))
		assert.Assert(t, !taken.Spec.Running, "%s: the unlabelled taken LimaVM was started", step)
	}
	setApp(false, true, metav1.ConditionFalse)
	assertRunning("App stopped", false)
	setApp(true, false, metav1.ConditionTrue)
	assertRunning("Kubernetes disabled", false)
	var staleVMs limav1alpha1.LimaVMList
	assert.NilError(t, c.List(ctx, &staleVMs, client.HasLabels{machineUIDLabel}))
	setApp(true, true, metav1.ConditionTrue)
	assertRunning("App running with Kubernetes", true)

	// This pass reads the LimaVMs from a cache that does not have the
	// previous pass's write yet.
	stale := *r
	stale.Client = interceptor.NewClient(c, interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if vms, ok := list.(*limav1alpha1.LimaVMList); ok {
				staleVMs.DeepCopyInto(vms)
				return nil
			}
			return cl.List(ctx, list, opts...)
		},
	})
	_, err = stale.Reconcile(ctx, req)
	assert.NilError(t, err)
	assertRunning("stale LimaVM cache", true)

	// The node VMs and input ConfigMaps of another guest cluster, or of a
	// LimaMachine that is gone, are removed even when they have an owned
	// finalizer.
	goneLabels := maps.Clone(wantLabels)
	goneLabels[machineUIDLabel] = "unknown-machine"
	staleLabels := maps.Clone(wantLabels)
	staleLabels[guestClusterUIDLabel] = "old-cluster"
	ownedFinalizer := []string{base.OwnedFinalizerFor("LimaMachine")}
	garbage := []client.Object{
		&limav1alpha1.LimaVM{Namespace: appv1alpha1.AppNamespace, Name: "gone", Labels: goneLabels, Finalizers: ownedFinalizer},
		&limav1alpha1.LimaVM{Namespace: appv1alpha1.AppNamespace, Name: "stale", Labels: staleLabels, Finalizers: ownedFinalizer},
		&corev1.ConfigMap{Namespace: appv1alpha1.AppNamespace, Name: "gone-source", Labels: goneLabels},
	}
	for _, obj := range garbage {
		assert.NilError(t, c.Create(ctx, obj))
	}
	r.probes["unknown-machine"] = probeRecord{}
	_, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	for _, obj := range garbage {
		err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		assert.Assert(t, apierrors.IsNotFound(err), "garbage %s was not removed: %v", obj.GetName(), err)
	}
	_, ok := r.probes["unknown-machine"]
	assert.Assert(t, !ok, "the probe of an unknown LimaMachine was kept")
	assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(vm), vm), "happy's LimaVM")
	assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(taken), taken), "the taken LimaVM")

	// Deleting a LimaMachine deletes its node VM and input ConfigMap, and the
	// LimaMachine stays until the LimaVM is gone. The LimaVM controller
	// doesn't run here, so the test adds its cleanup finalizer.
	assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(vm), vm))
	vm.Finalizers = append(vm.Finalizers, base.CleanupFinalizerName)
	assert.NilError(t, c.Update(ctx, vm))
	lingering := &corev1.ConfigMap{Namespace: appv1alpha1.AppNamespace, Name: "happy-source", Labels: wantLabels}
	assert.NilError(t, c.Create(ctx, lingering))
	assert.NilError(t, guest.Delete(ctx, &happy))
	result, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	assert.Equal(t, result, ctrl.Result{RequeueAfter: requeueInProgress})
	assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(vm), vm))
	assert.Assert(t, !vm.DeletionTimestamp.IsZero(), "happy's LimaVM is not being deleted")
	assert.DeepEqual(t, vm.Finalizers, []string{base.CleanupFinalizerName})
	err = c.Get(ctx, client.ObjectKeyFromObject(lingering), lingering)
	assert.Assert(t, apierrors.IsNotFound(err), "happy's input ConfigMap was not deleted: %v", err)
	assert.NilError(t, guest.Get(ctx, client.ObjectKeyFromObject(&happy), &happy), "the happy LimaMachine is gone before its LimaVM")

	vm.Finalizers = nil
	assert.NilError(t, c.Update(ctx, vm))
	_, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	err = guest.Get(ctx, client.ObjectKeyFromObject(&happy), &happy)
	assert.Assert(t, apierrors.IsNotFound(err), "the happy LimaMachine kept its finalizer: %v", err)
	_, ok = r.probes[happy.UID]
	assert.Assert(t, !ok, "the probe of the deleted happy LimaMachine was kept")

	// Deleting a LimaMachine whose LimaVM belongs to something else leaves
	// that LimaVM alone.
	assert.NilError(t, guest.Delete(ctx, &capiv1alpha1.LimaMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "taken"}}))
	_, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	err = guest.Get(ctx, client.ObjectKey{Namespace: "default", Name: "taken"}, &capiv1alpha1.LimaMachine{})
	assert.Assert(t, apierrors.IsNotFound(err), "the taken LimaMachine kept its finalizer: %v", err)
	assert.NilError(t, c.Get(ctx, client.ObjectKeyFromObject(taken), taken), "the unlabelled taken LimaVM")
}

// TestReconcileLimaMachinesFailedVM checks that a LimaMachine whose node VM
// rdd fails to create or to remove doesn't stop the other LimaMachines. It
// also checks that a LimaVM name used in another namespace is a NameConflict,
// and that a failed LimaVM create deletes the input ConfigMap, so the next
// pass renders the template again.
func TestReconcileLimaMachinesFailedVM(t *testing.T) {
	env := testapiserver.Start(t)
	ctx := logf.IntoContext(t.Context(), testr.New(t))
	clientset, err := apiextensionsclientset.NewForConfig(env.Config)
	assert.NilError(t, err)
	guestCRDs := clientset.ApiextensionsV1().CustomResourceDefinitions()
	for _, crd := range []*apiextensionsv1.CustomResourceDefinition{
		machineCRD(),
		standInCRD(clusterGVK.Group, clusterGVK.Version, clusterGVK.Kind, "clusters"),
		standInCRD("rke.cattle.io", "v1", "RKEControlPlane", "rkecontrolplanes"),
	} {
		_, err := guestCRDs.Create(ctx, crd, metav1.CreateOptions{})
		assert.NilError(t, err)
		assert.NilError(t, crds.WaitEstablished(ctx, guestCRDs, crd.Name, time.Minute))
	}
	k3sConfigPath := filepath.Join(t.TempDir(), "k3s.yaml")
	assert.NilError(t, os.WriteFile(k3sConfigPath, guestKubeConfig(t, env), 0o600))

	scheme := k8sruntime.NewScheme()
	assert.NilError(t, appv1alpha1.AddToScheme(scheme))
	assert.NilError(t, corev1.AddToScheme(scheme))
	assert.NilError(t, limav1alpha1.AddToScheme(scheme))
	conflict := &limav1alpha1.LimaVM{Namespace: "default", Name: "a-conflict"}
	apiServer := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newApp(metav1.ConditionTrue, metav1.ConditionTrue), conflict).Build()
	// apiServer is the API reader, and c the cached client in front of it.
	// That cache keeps returning a-broken's input ConfigMap after the
	// ConfigMap is deleted.
	broken := true
	stuck := false
	var cachedInput *corev1.ConfigMap
	c := interceptor.NewClient(apiServer, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if _, ok := obj.(*limav1alpha1.LimaVM); ok && stuck && obj.GetName() == "a-broken" {
				return apierrors.NewInternalError(errors.New("stuck"))
			}
			return c.Update(ctx, obj, opts...)
		},
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*limav1alpha1.LimaVM); ok && broken && obj.GetName() == "a-broken" {
				return apierrors.NewInternalError(errors.New("broken"))
			}
			if cm, ok := obj.(*corev1.ConfigMap); ok && cm.Name == "a-broken"+inputConfigMapSuffix {
				cachedInput = cm.DeepCopy()
			}
			return c.Create(ctx, obj, opts...)
		},
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if cm, ok := obj.(*corev1.ConfigMap); ok && cachedInput != nil && key == client.ObjectKeyFromObject(cachedInput) {
				cachedInput.DeepCopyInto(cm)
				return nil
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
	r := &Reconciler{Client: c, apiReader: apiServer, K3sConfigPath: k3sConfigPath, LimaHome: "/l"}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName}}
	// The first pass installs our CRDs.
	_, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)

	guest, err := client.New(env.Config, client.Options{Scheme: guestScheme})
	assert.NilError(t, err)
	assert.NilError(t, guest.Create(ctx, &corev1.Secret{
		Namespace: "default", Name: "bootstrap",
		Data: map[string][]byte{"value": cloudConfigFor("", gzipBase64(t, []byte("true\n")), "", "")},
	}))
	cluster := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{
		"controlPlaneRef": map[string]any{"apiGroup": "rke.cattle.io", "kind": "RKEControlPlane", "name": "k3s"},
	}}}
	cluster.SetGroupVersionKind(clusterGVK)
	cluster.SetNamespace("default")
	cluster.SetName("k3s")
	assert.NilError(t, guest.Create(ctx, cluster))
	controlPlane := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"kubernetesVersion": "v1.36.6+k3s1"}}}
	controlPlane.SetGroupVersionKind(schema.GroupVersionKind{Group: "rke.cattle.io", Version: "v1", Kind: "RKEControlPlane"})
	controlPlane.SetNamespace("default")
	controlPlane.SetName("k3s")
	assert.NilError(t, guest.Create(ctx, controlPlane))
	createLimaMachine := func(name string) {
		t.Helper()
		machine := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{
			"clusterName": "k3s", "bootstrap": map[string]any{"dataSecretName": "bootstrap"},
		}}}
		machine.SetGroupVersionKind(machineGVK)
		machine.SetNamespace("default")
		machine.SetName(name)
		assert.NilError(t, guest.Create(ctx, machine))
		assert.NilError(t, guest.Create(ctx, &capiv1alpha1.LimaMachine{ObjectMeta: metav1.ObjectMeta{
			Namespace: "default", Name: name,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: machineGVK.GroupVersion().String(), Kind: machineGVK.Kind, Name: name, UID: machine.GetUID(),
			}},
		}}))
	}
	// The guest cluster lists LimaMachines by name, so the failing ones come
	// first.
	for _, name := range []string{"a-broken", "a-conflict", "b-happy"} {
		createLimaMachine(name)
	}

	_, err = r.Reconcile(ctx, req)
	assert.ErrorContains(t, err, "a-broken")
	assert.NilError(t, c.Get(ctx, client.ObjectKey{Namespace: appv1alpha1.AppNamespace, Name: "b-happy"}, &limav1alpha1.LimaVM{}),
		"b-happy's node VM")
	var limaMachine capiv1alpha1.LimaMachine
	assert.NilError(t, guest.Get(ctx, client.ObjectKey{Namespace: "default", Name: "a-conflict"}, &limaMachine))
	ready := apimeta.FindStatusCondition(limaMachine.Status.Conditions, capiv1alpha1.ReadyCondition)
	assert.Assert(t, ready != nil)
	assert.Equal(t, ready.Reason, capiv1alpha1.NameConflictReason)
	assert.Assert(t, strings.Contains(ready.Message, "default/a-conflict"), ready.Message)
	err = c.Get(ctx, client.ObjectKey{Namespace: appv1alpha1.AppNamespace, Name: "a-conflict"}, &limav1alpha1.LimaVM{})
	assert.Assert(t, apierrors.IsNotFound(err), "a-conflict's node VM: %v", err)
	for _, name := range []string{"a-conflict", "a-broken"} {
		err = apiServer.Get(ctx, client.ObjectKey{Namespace: appv1alpha1.AppNamespace, Name: name + inputConfigMapSuffix}, &corev1.ConfigMap{})
		assert.Assert(t, apierrors.IsNotFound(err), "%s's input ConfigMap: %v", name, err)
	}

	// The cache still holds a-broken's deleted input ConfigMap, but the next
	// pass creates the ConfigMap again from a newly rendered template.
	broken = false
	_, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	assert.NilError(t, apiServer.Get(ctx, client.ObjectKey{Namespace: appv1alpha1.AppNamespace, Name: "a-broken" + inputConfigMapSuffix}, &corev1.ConfigMap{}),
		"a-broken's input ConfigMap")

	// A deleted LimaMachine whose node VM rdd fails to remove doesn't hold
	// back the LimaMachines after it either.
	stuck = true
	assert.NilError(t, guest.Delete(ctx, &capiv1alpha1.LimaMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "a-broken"}}))
	createLimaMachine("c-late")
	_, err = r.Reconcile(ctx, req)
	assert.ErrorContains(t, err, "stuck")
	assert.NilError(t, c.Get(ctx, client.ObjectKey{Namespace: appv1alpha1.AppNamespace, Name: "c-late"}, &limav1alpha1.LimaVM{}),
		"c-late's node VM")
}

// guestKubeConfig returns a kubeconfig with full access to env.
func guestKubeConfig(t *testing.T, env *envtest.Environment) []byte {
	t.Helper()
	user, err := env.AddUser(envtest.User{Name: "capi-test", Groups: []string{"system:masters"}}, nil)
	assert.NilError(t, err)
	kubeConfig, err := user.KubeConfig()
	assert.NilError(t, err)
	return kubeConfig
}

// setConditionsTrue sets every condition of app to True.
func setConditionsTrue(app *appv1alpha1.App) {
	for i := range app.Status.Conditions {
		app.Status.Conditions[i].Status = metav1.ConditionTrue
	}
}

// established reports whether the API server serves crd.
func established(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, condition := range crd.Status.Conditions {
		if condition.Type == apiextensionsv1.Established {
			return condition.Status == apiextensionsv1.ConditionTrue
		}
	}
	return false
}

// machineCRD returns a minimal stand-in for Cluster API's Machine CRD.
func machineCRD() *apiextensionsv1.CustomResourceDefinition {
	return standInCRD("cluster.x-k8s.io", "v1beta2", "Machine", "machines")
}

// standInCRD returns a minimal namespaced CRD for kind in group and version
// that accepts any fields.
func standInCRD(group, version, kind, plural string) *apiextensionsv1.CustomResourceDefinition {
	preserve := true
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: plural + "." + group},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:   kind,
				Plural: plural,
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    version,
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type:                   "object",
						XPreserveUnknownFields: &preserve,
					},
				},
			}},
		},
	}
}
