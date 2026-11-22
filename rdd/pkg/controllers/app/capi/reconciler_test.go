// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/event"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	limav1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
	guestcapi "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi"
	capiv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
	limavmcontrollers "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/lima/limavm/controllers"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/crds"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/testapiserver"
)

// newApp returns the App with the Running and KubernetesReady conditions set
// to the given statuses.
func newApp(running, kubernetesReady metav1.ConditionStatus) *appv1alpha1.App {
	return &appv1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{Name: appName, Generation: 1},
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
	// at all shows the client was rebuilt from the changed k3s.yaml.
	assert.NilError(t, os.WriteFile(k3sConfigPath, kubeConfig, 0o600))
	result, err = r.Reconcile(ctx, req)
	assert.NilError(t, err)
	assert.Equal(t, result, ctrl.Result{RequeueAfter: requeueIdle})
	for _, crd := range ourCRDs {
		_, err := guestCRDs.Get(ctx, crd.Name, metav1.GetOptions{})
		assert.Assert(t, apierrors.IsNotFound(err), "%s: %v", crd.Name, err)
	}

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
	guest, err := client.New(env.Config, client.Options{Scheme: guestScheme})
	assert.NilError(t, err)
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
	ctx := t.Context()
	clientset, err := apiextensionsclientset.NewForConfig(env.Config)
	assert.NilError(t, err)
	_, err = clientset.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, machineCRD(), metav1.CreateOptions{})
	assert.NilError(t, err)
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
	// createMachine creates a LimaMachine. Unless version is empty, it also
	// creates a Machine with the given version and bootstrap Secret name, and
	// makes that Machine the LimaMachine's owner.
	createMachine := func(name, version, secretName string) {
		t.Helper()
		limaMachine := &capiv1alpha1.LimaMachine{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name}}
		if version != "" {
			machine := &unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{"version": version, "bootstrap": map[string]any{"dataSecretName": secretName}},
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
	const k3s = "v1.36.6+k3s1"
	longName := strings.Repeat("a", 60)
	createMachine("happy", k3s, "bootstrap")
	createMachine("orphan", "", "")
	createMachine("rke2", "v1.36.6+rke2r1", "bootstrap")
	createMachine("nosecret", k3s, "")
	createMachine("badsecret", k3s, "unsupported")
	createMachine(longName, k3s, "bootstrap")
	createMachine("taken", k3s, "bootstrap")

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

	result, err := r.Reconcile(ctx, req)
	assert.NilError(t, err)
	assert.Equal(t, result, ctrl.Result{RequeueAfter: requeueInProgress})
	assertReady("happy", capiv1alpha1.ProvisioningReason)
	assertReady("orphan", capiv1alpha1.WaitingForMachineOwnerReason)
	assertReady("rke2", capiv1alpha1.UnsupportedDistributionReason)
	assertReady("nosecret", capiv1alpha1.WaitingForBootstrapDataReason)
	assertReady("badsecret", capiv1alpha1.UnsupportedBootstrapDataReason)
	assertReady(longName, capiv1alpha1.InvalidNameReason)
	assertReady("taken", capiv1alpha1.NameConflictReason)

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
	preserve := true
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: machineCRDName},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "cluster.x-k8s.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:   "Machine",
				Plural: "machines",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1beta2",
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
