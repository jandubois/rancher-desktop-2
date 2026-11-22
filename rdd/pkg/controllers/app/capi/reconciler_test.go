// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/event"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	guestcapi "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi"
	capiv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi/v1alpha1"
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

	user, err := env.AddUser(envtest.User{Name: "capi-test", Groups: []string{"system:masters"}}, nil)
	assert.NilError(t, err)
	kubeConfig, err := user.KubeConfig()
	assert.NilError(t, err)

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
	r := &Reconciler{Client: c, K3sConfigPath: k3sConfigPath}
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
