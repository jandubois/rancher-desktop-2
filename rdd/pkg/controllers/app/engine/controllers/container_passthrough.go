// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"al.essio.dev/pkg/shellescape"
	"github.com/containerd/errdefs"
	"github.com/gorilla/websocket"
	"github.com/moby/moby/api/pkg/stdcopy"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	containersv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestexec"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/terminal"
)

// validContainerMirrorName reports whether the path segment can name a
// Container mirror. Every backend derives that name to be a valid Kubernetes
// object name, so the object-name rule is both the widest check that rejects
// a path traversal and the narrowest one that accepts every mirror: moby's
// hex ID, containerd's "ctr-" hash, and a containerd ID kept verbatim.
func validContainerMirrorName(name string) bool {
	return len(validation.IsDNS1123Subdomain(name)) == 0
}

// containerShellScript runs bash when the container has it, and sh otherwise.
const containerShellScript = "if command -v bash >/dev/null; then exec bash; fi; exec sh"

// containerShellArgs returns the `exec` subcommand and its arguments for a
// `docker` or `nerdctl` command line that opens an interactive shell in the
// container with this ID.
func containerShellArgs(id string) string {
	return "exec -it -e TERM=xterm-256color " + shellescape.Quote(id) +
		" sh -c " + shellescape.Quote(containerShellScript)
}

// HandleExec implements the endpoint that opens a shell in a container.
// It is at `/passthrough/engine/exec/{container}`.
func (r *EngineReconciler) HandleExec(w http.ResponseWriter, req *http.Request) {
	log := ctrl.LoggerFrom(req.Context())
	name, _, _ := strings.Cut(strings.TrimLeft(req.URL.Path, "/"), "/")
	if !validContainerMirrorName(name) {
		log.V(5).Info("Invalid container ID", "container", name)
		http.Error(w, "Invalid container ID", http.StatusBadRequest)
		return
	}
	// Attach calls the start function only for a new session. Keep the checks
	// of the engine and the container in startContainerShell, so that a
	// reattach to a running shell skips them and works while the engine
	// watcher is down.
	r.shells.Attach(w, req, name, func(ctx context.Context, columns, rows int) (terminal.Shell, error) {
		return r.startContainerShell(ctx, name, columns, rows)
	})
}

// startContainerShell starts a shell in the container whose Container mirror
// is called name. It first checks that the engine and the container are
// running.
func (r *EngineReconciler) startContainerShell(ctx context.Context, name string, columns, rows int) (terminal.Shell, error) {
	r.engineMu.Lock()
	engine := r.engine
	r.engineMu.Unlock()
	if engine == nil {
		return nil, errors.New("the container engine is not running")
	}

	var c containersv1alpha1.Container
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: appv1alpha1.AppNamespace, Name: name}, &c)
	if apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("container %q not found", name)
	} else if err != nil {
		return nil, fmt.Errorf("failed to get container %q: %w", name, err)
	}
	if c.Status.Status != containersv1alpha1.ContainerStatusRunning {
		return nil, fmt.Errorf("container %q is not running (status: %s)", name, c.Status.Status)
	}

	startPTY := r.startPTY
	if startPTY == nil {
		startPTY = startAppVMPTY
	}
	return startPTY(ctx, engine.shellCommand(&c), columns, rows)
}

// startAppVMPTY runs the command under a pty in the App's VM.
func startAppVMPTY(ctx context.Context, command string, columns, rows int) (terminal.Shell, error) {
	inst, err := guestexec.Inspect(ctx, appv1alpha1.AppLimaVMName)
	if err != nil {
		return nil, err
	}
	return guestexec.StartPTYSession(ctx, inst, command, columns, rows)
}

// HandleLogs implements the log endpoint to pass through container logs.
// It is at `/passthrough/engine/logs/{container}[?tail=999]`.
func (r *EngineReconciler) HandleLogs(w http.ResponseWriter, req *http.Request) {
	log := ctrl.LoggerFrom(req.Context())
	containerID, _, _ := strings.Cut(strings.TrimLeft(req.URL.Path, "/"), "/")
	if !validContainerMirrorName(containerID) {
		log.V(5).Info("Invalid container ID", "container", containerID)
		http.Error(w, "Invalid container ID", http.StatusBadRequest)
		return
	}
	log.V(5).Info("Handling logs for container", "containerID", containerID)

	var c containersv1alpha1.Container
	err := r.Client.Get(req.Context(), types.NamespacedName{
		Namespace: appv1alpha1.AppNamespace,
		Name:      containerID,
	}, &c)
	if err != nil {
		switch {
		case apierrors.IsNotFound(err):
			log.V(5).Info("Container not found", "container", containerID)
			http.Error(w, "Container not found", http.StatusNotFound)
		default:
			log.V(5).Info("Failed to get container", "container", containerID, "error", err)
			http.Error(w, "Failed to get container", http.StatusInternalServerError)
		}
		return
	}

	var opts []engineLogOptions

	if f := req.URL.Query().Get("follow"); f != "" {
		if follow, err := strconv.ParseBool(f); err == nil {
			opts = append(opts, engineLogWithFollow(follow))
		} else {
			log.V(5).Info("Invalid follow parameter", "follow", f, "error", err)
			http.Error(w, "Invalid follow parameter", http.StatusBadRequest)
			return
		}
	}
	if tail := req.URL.Query().Get("tail"); tail != "" {
		opts = append(opts, engineLogWithTail(tail))
	}

	r.engineMu.Lock()
	engine := r.engine
	r.engineMu.Unlock()
	if engine == nil {
		log.V(5).Info("Engine watcher not running")
		http.Error(w, "Engine watcher not running", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()

	hasTTY, err := engine.hasTTY(ctx, &c)
	if err != nil {
		switch {
		case errors.Is(err, errLogsNotSupported):
			log.V(5).Info("Engine does not support container logs")
			http.Error(w, errLogsNotSupported.Error(), http.StatusNotImplemented)
		case errdefs.IsNotFound(err):
			log.V(5).Info("Container not found", "container", containerID)
			http.Error(w, "Container not found", http.StatusNotFound)
		case errdefs.IsInvalidArgument(err):
			log.V(5).Info("Invalid argument", "error", err)
			http.Error(w, "Invalid argument", http.StatusBadRequest)
		default:
			log.V(5).Info("Failed to inspect container", "error", err)
			http.Error(w, "Failed to inspect container", http.StatusInternalServerError)
		}
		return
	}

	reader, err := engine.getLogs(ctx, &c, opts...)
	if err != nil {
		switch {
		case errors.Is(err, errLogsNotSupported):
			log.V(5).Info("Engine does not support container logs")
			http.Error(w, errLogsNotSupported.Error(), http.StatusNotImplemented)
		case errdefs.IsNotFound(err):
			log.V(5).Info("Container not found", "container", containerID)
			http.Error(w, "Container not found", http.StatusNotFound)
		case errdefs.IsInvalidArgument(err):
			log.V(5).Info("Invalid argument", "error", err)
			http.Error(w, "Invalid argument", http.StatusBadRequest)
		default:
			log.V(5).Info("Failed to get container logs", "error", err)
			http.Error(w, "Failed to get container logs", http.StatusInternalServerError)
		}
		return
	}
	defer reader.Close()

	// Now that we have everything, we can do the websocket upgrade.  Defer this
	// to as late as possible so we can report container engine errors via HTTP.
	conn, err := terminal.Upgrade(w, req, terminal.Options{ReadLimit: 4096})
	if err != nil {
		log.V(5).Info("Failed to upgrade to WebSocket", "error", err)
		return
	}
	// Stop reading the logs once the connection fails or the peer closes it.
	go func() {
		select {
		case <-conn.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	if hasTTY {
		_, err = io.Copy(conn, reader)
	} else {
		_, err = stdcopy.StdCopy(conn, conn, reader)
	}
	if err != nil {
		log.V(5).Info("Failed to copy container logs", "error", err)
		switch {
		case errors.Is(err, context.Canceled):
		case errors.Is(err, net.ErrClosed):
		case errors.Is(err, websocket.ErrCloseSent):
		case errors.As(err, new(*websocket.CloseError)):
		default:
			conn.Close(websocket.CloseInternalServerErr, fmt.Sprintf("failed to copy container logs: %s", err))
			return
		}
	}
	conn.Close(websocket.CloseNormalClosure, "Closing connection")
}
