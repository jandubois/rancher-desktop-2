// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	guestcapi "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi"
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
	// cluster cannot be reached or installing our CRDs failed.
	requeueInProgress = 5 * time.Second

	// requeueIdle is the wait between passes while the guest cluster lacks
	// Cluster API or already has our CRDs.
	requeueIdle = 30 * time.Second
)

// Reconciler installs the Lima infrastructure provider's CRDs into the App's
// k3s cluster once Cluster API is present there.
type Reconciler struct {
	// Client talks to rdd's control plane, which holds the App.
	client.Client

	// K3sConfigPath is the path of the App's k3s kubeconfig, k3s.yaml.
	K3sConfigPath string

	// Reconcile never runs concurrently with itself, because
	// MaxConcurrentReconciles keeps its default of 1. So the guest
	// connection cached below doesn't need a lock.

	// guestKubeConfig is the k3s.yaml content that crdClient was built from.
	guestKubeConfig []byte
	// crdClient reaches the guest cluster's CRDs.
	crdClient apiextensionsv1client.CustomResourceDefinitionInterface
	// crdsInstalled is set once our CRDs are installed through crdClient.
	crdsInstalled bool
}

// Reconcile installs our CRDs into the guest cluster when it is running and
// has Cluster API.
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
	r.guestKubeConfig = data
	r.crdClient = clientset.CustomResourceDefinitions()
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
		Named("capi-reconciler").
		Complete(r)
}
