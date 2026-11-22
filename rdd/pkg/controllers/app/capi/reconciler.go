// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	limav1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
	guestcapi "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi"
	capiv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/app/predicates"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/crds"
)

const (
	appName = "app"

	// machineCRDName is Cluster API's Machine CRD. Its presence in the guest
	// cluster tells us that CAPI is installed there.
	machineCRDName = "machines.cluster.x-k8s.io"

	// guestRequestTimeout bounds each request to the guest's API server.
	guestRequestTimeout = 10 * time.Second

	// crdEstablishTimeout bounds the wait for each CRD we create in the guest.
	crdEstablishTimeout = 30 * time.Second

	// requeueInProgress is the wait before the next pass when the guest
	// cluster cannot be reached, our CRDs cannot be installed, our
	// infrastructure objects cannot be read or updated, or a LimaMachine
	// waits for its Machine, its bootstrap data or its node VM.
	requeueInProgress = 5 * time.Second

	// requeueIdle is the wait between passes while the guest cluster lacks
	// Cluster API, or after a pass that didn't leave any LimaMachine waiting.
	// The reconciler doesn't watch the guest cluster, so a new LimaCluster or
	// LimaMachine waits up to this long before a pass picks it up.
	requeueIdle = 30 * time.Second
)

// guestScheme holds the types that guestClient reads and writes.
var guestScheme = newGuestScheme()

func newGuestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(capiv1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	return scheme
}

// Reconciler installs the Lima infrastructure provider's CRDs into the App's
// k3s cluster once Cluster API is present there, then marks every LimaCluster
// in that cluster provisioned and ready, and creates a node VM for every
// LimaMachine.
type Reconciler struct {
	// Client talks to rdd's control plane, which holds the App.
	client.Client
	// apiReader reads rdd's control plane without Client's cache.
	apiReader client.Reader

	// K3sConfigPath is the path of the App's k3s kubeconfig, k3s.yaml.
	K3sConfigPath string

	// LimaHome is rdd's LIMA_HOME, which holds the node VMs' instance
	// directories. It limits the length of a LimaMachine's name.
	LimaHome string

	// Reconcile never runs concurrently with itself, because
	// MaxConcurrentReconciles keeps its default of 1. So the guest
	// connection cached below doesn't need a lock.

	// guestKubeConfig is the k3s.yaml content that crdClient and guestClient
	// were built from.
	guestKubeConfig []byte
	// crdClient reaches the guest cluster's CRDs.
	crdClient apiextensionsv1client.CustomResourceDefinitionInterface
	// guestClient reaches our infrastructure objects in the guest cluster. It
	// reads straight from the guest's API server. A cached client would keep
	// copies of guest objects in rdd, and its watches would have to start and
	// stop with the guest cluster.
	guestClient client.Client
	// crdsInstalled is set once our CRDs are installed through crdClient.
	crdsInstalled bool

	// probeNode reads the state of a running node VM. RegisterWithManager
	// sets it to probeNodeOverSSH; tests replace it.
	probeNode func(ctx context.Context, vmName string) (nodeStatus, error)
	// probes holds the last complete probe of each LimaMachine's node VM,
	// keyed by the LimaMachine's UID.
	probes map[types.UID]probeRecord
}

// probeRecord is a complete probe of a node VM, with the LimaVM's restart
// count at the time of the probe.
type probeRecord struct {
	restartCount int32
	status       nodeStatus
}

// Reconcile installs our CRDs into the guest cluster when it is running and
// has Cluster API, then marks the LimaClusters there provisioned and ready,
// and creates the LimaMachines' node VMs. A failed request to the guest cluster
// leads to a requeue, unless the same pass also has an error from rdd's
// control plane. Then Reconcile returns both.
func (r *Reconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var app appv1alpha1.App
	if err := r.Get(ctx, client.ObjectKey{Name: appName}, &app); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !apimeta.IsStatusConditionTrue(app.Status.Conditions, appv1alpha1.AppConditionRunning) ||
		!apimeta.IsStatusConditionTrue(app.Status.Conditions, appv1alpha1.AppConditionKubernetesReady) {
		// The App predicate wakes us when either condition changes.
		return ctrl.Result{}, nil
	}

	if err := r.connectGuest(); err != nil {
		log.V(1).Info("Cannot load the guest kubeconfig", "err", err)
		return ctrl.Result{RequeueAfter: requeueInProgress}, nil
	}

	if _, err := r.crdClient.Get(ctx, machineCRDName, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			// A k3s without Cluster API stays untouched.
			log.V(1).Info("Cluster API is not installed in the guest cluster")
			return ctrl.Result{RequeueAfter: requeueIdle}, nil
		}
		log.V(1).Info("Cannot look up Cluster API's Machine CRD in the guest cluster", "err", err)
		return ctrl.Result{RequeueAfter: requeueInProgress}, nil
	}

	if !r.crdsInstalled {
		if err := r.installCRDs(ctx); err != nil {
			log.Error(err, "Failed to install CRDs into the guest cluster")
			return ctrl.Result{RequeueAfter: requeueInProgress}, nil
		}
		r.crdsInstalled = true
	}

	if err := r.reconcileLimaClusters(ctx); err != nil {
		log.V(1).Info("Cannot reconcile LimaClusters in the guest cluster", "err", err)
		return ctrl.Result{RequeueAfter: requeueInProgress}, nil
	}

	inProgress, err := r.reconcileLimaMachines(ctx, &app)
	if err != nil {
		// reconcileLimaMachines joins the errors of all LimaMachines, so returning
		// the joined error keeps every one of them in controller-runtime's log.
		if _, ok := errors.AsType[*controlPlaneError](err); ok {
			return ctrl.Result{}, err
		}
		log.V(1).Info("Cannot reconcile LimaMachines in the guest cluster", "err", err)
		return ctrl.Result{RequeueAfter: requeueInProgress}, nil
	}
	if inProgress {
		return ctrl.Result{RequeueAfter: requeueInProgress}, nil
	}
	return ctrl.Result{RequeueAfter: requeueIdle}, nil
}

// connectGuest builds a new guest client whenever k3s.yaml changes, for
// example when turning Kubernetes off and on again moves k3s to another
// port.
func (r *Reconciler) connectGuest() error {
	data, err := os.ReadFile(r.K3sConfigPath)
	if err != nil {
		return fmt.Errorf("read guest kubeconfig: %w", err)
	}
	if r.crdClient != nil && bytes.Equal(data, r.guestKubeConfig) {
		return nil
	}
	cfg, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return fmt.Errorf("parse guest kubeconfig: %w", err)
	}
	cfg.Timeout = guestRequestTimeout
	clientset, err := apiextensionsv1client.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("create guest CRD client: %w", err)
	}
	guestClient, err := client.New(cfg, client.Options{Scheme: guestScheme})
	if err != nil {
		return fmt.Errorf("create guest client: %w", err)
	}
	r.guestKubeConfig = data
	r.crdClient = clientset.CustomResourceDefinitions()
	r.guestClient = guestClient
	r.crdsInstalled = false
	return nil
}

// installCRDs creates or updates our CRDs in the guest cluster, then waits
// until the API server serves each newly created CRD.
func (r *Reconciler) installCRDs(ctx context.Context) error {
	log := logf.FromContext(ctx)
	decoded, err := crds.Decode(guestcapi.CRDs)
	if err != nil {
		return fmt.Errorf("decode CRDs: %w", err)
	}
	var created []string
	for _, crd := range decoded {
		outcome, err := crds.Apply(ctx, r.crdClient, &crd)
		if err != nil {
			return err
		}
		switch outcome {
		case crds.Created:
			log.Info("Installed CRD in the guest cluster", "crd", crd.Name)
			created = append(created, crd.Name)
		case crds.Updated:
			log.Info("Updated CRD in the guest cluster", "crd", crd.Name)
		case crds.Unchanged:
		}
	}
	for _, name := range created {
		if err := crds.WaitEstablished(ctx, r.crdClient, name, crdEstablishTimeout); err != nil {
			return err
		}
	}
	return nil
}

// reconcileLimaClusters marks every LimaCluster in the guest cluster
// provisioned and ready. A LimaCluster doesn't have infrastructure of its own,
// because the node VMs belong to LimaMachines. So it is provisioned as soon as
// we see it. reconcileLimaClusters skips the write for a LimaCluster that is
// already marked provisioned and ready.
func (r *Reconciler) reconcileLimaClusters(ctx context.Context) error {
	var list capiv1alpha1.LimaClusterList
	if err := r.guestClient.List(ctx, &list); err != nil {
		return fmt.Errorf("list LimaClusters: %w", err)
	}
	for i := range list.Items {
		cluster := &list.Items[i]
		changed := !ptr.Deref(cluster.Status.Initialization.Provisioned, false)
		cluster.Status.Initialization.Provisioned = ptr.To(true)
		if apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               capiv1alpha1.ReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             capiv1alpha1.ReadyReason,
			ObservedGeneration: cluster.Generation,
		}) {
			changed = true
		}
		if !changed {
			continue
		}
		// The update sends the resourceVersion that List returned, so it
		// fails if the LimaCluster changed since then. The next pass retries.
		if err := r.guestClient.Status().Update(ctx, cluster); err != nil {
			return fmt.Errorf("update LimaCluster %s/%s status: %w", cluster.Namespace, cluster.Name, err)
		}
	}
	return nil
}

// appPredicate passes an App update only when it can change what Reconcile
// does. That is a spec change, or a change in the status of the Running or
// KubernetesReady condition. Changes to other conditions, and changes to a
// condition's timestamps alone, do not pass.
func appPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
				return true
			}
			oldApp, okOld := e.ObjectOld.(*appv1alpha1.App)
			newApp, okNew := e.ObjectNew.(*appv1alpha1.App)
			if !okOld || !okNew {
				return true
			}
			for _, condition := range []string{appv1alpha1.AppConditionRunning, appv1alpha1.AppConditionKubernetesReady} {
				if conditionStatus(oldApp, condition) != conditionStatus(newApp, condition) {
					return true
				}
			}
			return false
		},
	}
}

// conditionStatus returns the status of app's condition, or "" when app lacks it.
func conditionStatus(app *appv1alpha1.App, conditionType string) metav1.ConditionStatus {
	if condition := apimeta.FindStatusCondition(app.Status.Conditions, conditionType); condition != nil {
		return condition.Status
	}
	return ""
}

// SetupWithManager registers the reconciler with the manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1alpha1.App{}, builder.WithPredicates(predicates.WatchEventLogger("capi-reconciler"), appPredicate())).
		// A change to a node VM starts a pass, so that the LimaMachine's
		// Ready condition follows the VM's status.
		Watches(&limav1alpha1.LimaVM{},
			handler.EnqueueRequestsFromMapFunc(func(context.Context, client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: appName}}}
			}),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
				_, ok := obj.GetLabels()[machineUIDLabel]
				return ok
			}))).
		// When Reconcile returns an error, controller-runtime ignores the
		// pass's RequeueAfter. It retries instead after a backoff that grows
		// with each failure of the same request. Every request uses the App's
		// key, so while one LimaMachine's error persists, that backoff also
		// decides when the next timed pass comes for every other LimaMachine.
		// Limiting the backoff to requeueIdle keeps those passes at most as
		// far apart as idle ones.
		WithOptions(crcontroller.Options{
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](5*time.Millisecond, requeueIdle),
		}).
		Named("capi-reconciler").
		Complete(r)
}
