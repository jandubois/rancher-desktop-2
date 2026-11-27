// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package limavm

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/lima/limavm/controllers"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestexec"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/terminal"
)

// shellEndpoint is the passthrough endpoint that opens a login shell in a LimaVM.
const shellEndpoint = "shell"

// handleShell implements the endpoint that opens a login shell in a LimaVM.
// It is at `/passthrough/limavm/shell/{namespace}/{name}`.
func (c *controller) handleShell(w http.ResponseWriter, req *http.Request) {
	log := ctrl.LoggerFrom(req.Context())
	segments := strings.Split(strings.TrimLeft(req.URL.Path, "/"), "/")
	if len(segments) != 2 ||
		len(validation.IsDNS1123Label(segments[0])) != 0 ||
		len(validation.IsDNS1123Subdomain(segments[1])) != 0 {
		log.V(5).Info("Invalid LimaVM path", "path", req.URL.Path)
		http.Error(w, "Invalid LimaVM namespace or name", http.StatusBadRequest)
		return
	}
	namespace, name := segments[0], segments[1]
	// Leave the LimaVM checks to startShell. A browser can't read the HTTP
	// status of a failed WebSocket upgrade, but Attach sends an error from
	// startShell to the client on channel 3.
	c.shells.Attach(w, req, namespace+"/"+name, func(ctx context.Context, columns, rows int) (terminal.Shell, error) {
		return c.startShell(ctx, namespace, name, columns, rows)
	})
}

// startShell starts a login shell in the LimaVM after checking that it is
// running.
func (c *controller) startShell(ctx context.Context, namespace, name string, columns, rows int) (terminal.Shell, error) {
	var vm v1alpha1.LimaVM
	err := c.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &vm)
	if apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("LimaVM %q not found in namespace %q", name, namespace)
	} else if err != nil {
		return nil, fmt.Errorf("failed to get LimaVM %q in namespace %q: %w", name, namespace, err)
	}
	if !apimeta.IsStatusConditionTrue(vm.Status.Conditions, controllers.ConditionRunning) {
		return nil, fmt.Errorf("LimaVM %q is not running", name)
	}

	// The Lima instance has the LimaVM's name, which is unique across namespaces.
	inst, err := guestexec.Inspect(ctx, vm.Name)
	if err != nil {
		return nil, err
	}
	return guestexec.StartPTYSession(ctx, inst, `exec "$SHELL" --login`, columns, rows)
}
