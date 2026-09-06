// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"syscall"
	"time"

	containerdclient "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/go-logr/logr"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	containersv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
)

// processContainerAction handles a container carrying the AnnotationAction
// annotation. It dispatches the containerd call, records the outcome in
// status.lastAction, and then removes the annotation.
//
// The containerd call runs before the status and metadata patches so that a
// crash mid-flight leaves the annotation in place and the next reconcile
// replays the action. Start, stop, pause, and unpause are idempotent
// against a container already in the target state, so replay is safe.
// Restart has no target state to match: a replay recreates the task a second
// time, which the controller cannot distinguish from a deliberate re-request.
func (w *containerdWatcher) processContainerAction(ctx context.Context, c *containersv1alpha1.Container) error {
	raw, ok := c.Annotations[containersv1alpha1.AnnotationAction]
	if !ok {
		return nil
	}

	log := logf.FromContext(ctx).WithName("containerd-watcher")
	action := containersv1alpha1.ContainerAction(raw)
	observedAt := metav1.Now()

	// The webhook rejects invalid action values, but one written while the
	// webhook is offline can still reach storage. Drop such values here;
	// otherwise the CRD enum rejects the status.lastAction write, the
	// annotation stays in place, and every reconcile retries forever.
	if !action.IsValid() {
		log.Info("Dropping invalid container action annotation", "id", c.Name, "action", raw)
		return w.removeActionAnnotation(ctx, c, raw)
	}

	dispatchErr := w.dispatchContainerAction(ctx, log, c, action)

	lastAction := &containersv1alpha1.ContainerLastAction{
		Action:      action,
		ObservedAt:  observedAt,
		CompletedAt: metav1.Now(),
	}
	if dispatchErr == nil {
		lastAction.State = containersv1alpha1.ContainerActionSucceeded
	} else {
		lastAction.State = containersv1alpha1.ContainerActionFailed
		lastAction.Error = dispatchErr.Error()
		log.Info("Container action failed", "id", c.Name, "action", action, "error", dispatchErr)
	}

	latest, err := w.patchContainerLastAction(ctx, c.Name, lastAction)
	if err != nil {
		return fmt.Errorf("failed to patch lastAction for %s: %w", c.Name, err)
	}
	if latest == nil {
		// Mirror was deleted between dispatch and the status patch; nothing
		// left to clean up.
		return nil
	}
	if err := w.removeActionAnnotation(ctx, latest, raw); err != nil {
		return fmt.Errorf("failed to remove action annotation for %s: %w", c.Name, err)
	}
	return nil
}

// dispatchContainerAction executes the containerd call for a single action.
// The caller pre-validates the action name; the trailing error fires only
// when a new ContainerAction value is added to the type but not to the switch.
func (w *containerdWatcher) dispatchContainerAction(ctx context.Context, log logr.Logger, c *containersv1alpha1.Container, action containersv1alpha1.ContainerAction) error {
	ns := c.Status.Namespace
	if ns == "" {
		// A mirror without a namespace has no engine object to act on.
		return errors.New("container mirror has no namespace")
	}

	ctr, err := w.resolveContainer(ctx, ns, c.Name)
	if err != nil {
		return err
	}
	nsCtx := namespaces.WithNamespace(ctx, ns)

	switch action {
	case containersv1alpha1.ContainerActionStart:
		log.Info("Starting container", "id", c.Name)
		return w.startContainer(nsCtx, log, ctr)
	case containersv1alpha1.ContainerActionStop:
		log.Info("Stopping container", "id", c.Name)
		return w.stopTask(nsCtx, log, ctr)
	case containersv1alpha1.ContainerActionPause:
		log.Info("Pausing container", "id", c.Name)
		return w.pauseContainer(nsCtx, ctr)
	case containersv1alpha1.ContainerActionUnpause:
		log.Info("Unpausing container", "id", c.Name)
		return w.unpauseContainer(nsCtx, ctr)
	case containersv1alpha1.ContainerActionRestart:
		log.Info("Restarting container", "id", c.Name)
		return w.restartContainer(nsCtx, log, ctr)
	}
	return fmt.Errorf("unknown container action %q", action)
}

// resolveContainer maps a Container mirror name back to its containerd
// container. The mirror name normally IS the container ID, so LoadContainer
// hits directly. IDs that are not valid K8s names get hashed mirror names
// (see containerdMirrorName), which only a scan of the namespace can map back.
func (w *containerdWatcher) resolveContainer(ctx context.Context, ns, mirrorName string) (containerdclient.Container, error) {
	nsCtx := namespaces.WithNamespace(ctx, ns)
	ctr, err := w.cli.LoadContainer(nsCtx, mirrorName)
	if err == nil {
		return ctr, nil
	}
	if !errdefs.IsNotFound(err) {
		return nil, fmt.Errorf("failed to load container %s: %w", mirrorName, err)
	}

	ctrs, listErr := w.cli.Containers(nsCtx)
	if listErr != nil {
		return nil, fmt.Errorf("failed to list containers in namespace %s: %w", ns, listErr)
	}
	for _, candidate := range ctrs {
		if containerdMirrorName(ns, candidate.ID()) == mirrorName {
			return candidate, nil
		}
	}
	return nil, err
}

// startContainer starts the container's task, matching Docker's no-op
// semantics: a start on an already-running container returns nil.
func (w *containerdWatcher) startContainer(nsCtx context.Context, log logr.Logger, ctr containerdclient.Container) error {
	task, err := ctr.Task(nsCtx, nil)
	if errdefs.IsNotFound(err) {
		return w.startTask(nsCtx, log, ctr)
	}
	if err != nil {
		return fmt.Errorf("failed to load task: %w", err)
	}
	st, err := task.Status(nsCtx)
	if err != nil {
		return fmt.Errorf("failed to get task status: %w", err)
	}
	switch st.Status {
	case containerdclient.Running, containerdclient.Paused, containerdclient.Pausing:
		// Already running or paused: nothing to start.
		return nil
	case containerdclient.Created:
		return task.Start(nsCtx)
	case containerdclient.Stopped:
		if _, err := task.Delete(nsCtx); err != nil && !errdefs.IsNotFound(err) {
			return fmt.Errorf("failed to delete stopped task: %w", err)
		}
		return w.startTask(nsCtx, log, ctr)
	}
	return fmt.Errorf("cannot start container in state %q", st.Status)
}

// startTask creates and starts a fresh task for the container. Shared by
// start and restart.
//
// nerdctl records its log driver as a binary:// URI in the nerdctl/log-uri
// label; recreating the task with it keeps `nerdctl logs` working, and NullIO
// covers containers created without it (e.g. via ctr).
func (w *containerdWatcher) startTask(nsCtx context.Context, log logr.Logger, ctr containerdclient.Container) error {
	info, err := ctr.Info(nsCtx)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}
	creator, err := containerdIOCreator(log, info)
	if err != nil {
		return err
	}

	task, err := ctr.NewTask(nsCtx, creator)
	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}
	if err := task.Start(nsCtx); err != nil {
		// Best-effort cleanup so a half-created task cannot block the next
		// attempt.
		if _, delErr := task.Delete(nsCtx, containerdclient.WithProcessKill); delErr != nil {
			log.Error(delErr, "Failed to clean up half-created task", "id", ctr.ID())
		}
		return fmt.Errorf("failed to start task: %w", err)
	}
	return nil
}

// containerdIOCreator picks the IO for a task being recreated. The creator
// carries the terminal flag into CreateTaskRequest, and the shim only
// allocates a console when it is set, so a container whose OCI spec requests
// a terminal must get a terminal-aware creator or the runtime refuses to
// create it.
func containerdIOCreator(log logr.Logger, info containers.Container) (cio.Creator, error) {
	var logURI *url.URL
	// nerdctl writes the literal "none" for `--log-driver none`, which parses
	// as a relative URL and would reach the shim as a fifo path it cannot
	// open. Treat it, like an unparsable value, as no log driver at all.
	if raw := info.Labels["nerdctl/log-uri"]; raw != "" && raw != "none" {
		u, err := url.Parse(raw)
		if err != nil {
			log.V(1).Info("Ignoring unparsable log URI",
				"id", info.ID, "uri", raw, "error", err)
		} else {
			logURI = u
		}
	}

	if !containerdWantsTerminal(log, info) {
		if logURI != nil {
			return cio.LogURI(logURI), nil
		}
		return cio.NullIO, nil
	}
	if logURI == nil {
		// Every creator that allocates a console is driven by the log
		// driver, so without one there is nothing to attach the console to.
		// Say so here, rather than let the runtime refuse the task with a
		// console-socket error that names none of this.
		return nil, errors.New("cannot start a terminal container that records no log driver")
	}
	return cio.TerminalLogURI(logURI), nil
}

// containerdWantsTerminal reports whether the container's OCI spec asks for a
// terminal. An unreadable spec falls back to false, matching the behaviour of
// a container that never requested one.
func containerdWantsTerminal(log logr.Logger, info containers.Container) bool {
	if info.Spec == nil {
		return false
	}
	var spec oci.Spec
	if err := json.Unmarshal(info.Spec.GetValue(), &spec); err != nil {
		log.V(1).Info("Assuming no terminal", "id", info.ID, "error", err)
		return false
	}
	return spec.Process != nil && spec.Process.Terminal
}

// stopTask asks the container's task to exit with SIGTERM, waiting a grace
// period before escalating to SIGKILL. Shared by stop and restart. The stopped
// task is left in place: nerdctl derives the Exited state from it.
func (w *containerdWatcher) stopTask(nsCtx context.Context, log logr.Logger, ctr containerdclient.Container) error {
	task, err := ctr.Task(nsCtx, nil)
	if errdefs.IsNotFound(err) {
		// No task: nothing to stop, matching Docker's no-op semantics.
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to load task: %w", err)
	}
	st, err := task.Status(nsCtx)
	if err != nil {
		return fmt.Errorf("failed to get task status: %w", err)
	}
	switch st.Status {
	case containerdclient.Stopped, containerdclient.Created:
		// Stopped already, or created but never started: nothing to stop.
		return nil
	}

	// A paused task cannot act on a signal, so thaw it before asking it to
	// exit; otherwise the grace period always expires and the task is killed.
	// An unpause between the status read and here leaves Resume rejecting a
	// task that is already running. That is harmless; the signal and the
	// grace period still run.
	if st.Status == containerdclient.Paused || st.Status == containerdclient.Pausing {
		if err := task.Resume(nsCtx); err != nil {
			log.V(1).Info("Signaling a task that could not be resumed",
				"id", ctr.ID(), "error", err)
		}
	}

	// Wait must be armed before Kill so the exit is observed.
	exitCh, err := task.Wait(nsCtx)
	if err != nil {
		return fmt.Errorf("failed to wait for task: %w", err)
	}
	// A task that exits on its own between the status read and here answers
	// Kill with NotFound. The container did stop, which is what was asked
	// for, and the moby path reports that as success, so the mirror must not
	// say Failed either. Return without waiting on exitCh, because a Kill
	// that could not find the task is no guarantee an exit status is coming.
	if err := task.Kill(nsCtx, linuxSIGTERM); err != nil {
		if !errdefs.IsNotFound(err) {
			return fmt.Errorf("failed to signal task: %w", err)
		}
		return nil
	}
	select {
	case status := <-exitCh:
		// A failed Wait RPC arrives as an exit status carrying the error, so
		// dropping it would report a container that never exited as stopped.
		return status.Error()
	case <-time.After(10 * time.Second):
	}
	if err := task.Kill(nsCtx, linuxSIGKILL); err != nil {
		if !errdefs.IsNotFound(err) {
			return fmt.Errorf("failed to kill task: %w", err)
		}
		return nil
	}
	// No deadline here, and none inside task.Delete's WithProcessKill either:
	// both wait on the exit channel until the task reports. A task that
	// outlives SIGKILL holds the engine reconciler until the manager shuts
	// down. Bounding one and not the other only moves the wait, and a deadline
	// short enough to help would record a failed stop for a container that was
	// still on its way out; the moby path applies no host-side deadline at all.
	select {
	case status := <-exitCh:
		return status.Error()
	case <-nsCtx.Done():
		return nsCtx.Err()
	}
}

// linuxSIGTERM and linuxSIGKILL are the guest's numbers for the two signals
// the stop sequence sends.
const (
	linuxSIGTERM = syscall.Signal(15)
	linuxSIGKILL = syscall.Signal(9)
)

// pauseContainer pauses the container's task. Pausing an already-paused
// container returns nil: two reconcile ticks can read the same action
// annotation through the informer cache before the removal lands, and the
// pre-check keeps the second dispatch from flipping lastAction to Failed.
func (w *containerdWatcher) pauseContainer(nsCtx context.Context, ctr containerdclient.Container) error {
	task, err := ctr.Task(nsCtx, nil)
	if errdefs.IsNotFound(err) {
		return errors.New("cannot pause container: not running")
	}
	if err != nil {
		return fmt.Errorf("failed to load task: %w", err)
	}
	st, err := task.Status(nsCtx)
	if err != nil {
		return fmt.Errorf("failed to get task status: %w", err)
	}
	switch st.Status {
	case containerdclient.Paused:
		return nil
	case containerdclient.Running:
		return task.Pause(nsCtx)
	}
	return errors.New("cannot pause container: not running")
}

// unpauseContainer resumes the container's task. Resuming an already-running
// container returns nil, for the same informer-cache reason as pauseContainer.
func (w *containerdWatcher) unpauseContainer(nsCtx context.Context, ctr containerdclient.Container) error {
	task, err := ctr.Task(nsCtx, nil)
	if errdefs.IsNotFound(err) {
		return errors.New("cannot unpause container: not running")
	}
	if err != nil {
		return fmt.Errorf("failed to load task: %w", err)
	}
	st, err := task.Status(nsCtx)
	if err != nil {
		return fmt.Errorf("failed to get task status: %w", err)
	}
	switch st.Status {
	case containerdclient.Paused:
		return task.Resume(nsCtx)
	case containerdclient.Running:
		return nil
	}
	return errors.New("cannot unpause container: not running")
}

// restartContainer stops and recreates the container's task. Unlike Docker's
// restart, a task cannot be restarted in place; it is recreated. That also
// makes restart start a stopped container, matching Docker.
func (w *containerdWatcher) restartContainer(nsCtx context.Context, log logr.Logger, ctr containerdclient.Container) error {
	if err := w.stopTask(nsCtx, log, ctr); err != nil {
		return err
	}
	if task, err := ctr.Task(nsCtx, nil); err == nil {
		// WithProcessKill, because stopTask leaves a task that never started
		// in Created, and containerd refuses to delete one of those without
		// it. The sibling deletes pass it for the same reason.
		if _, err := task.Delete(nsCtx, containerdclient.WithProcessKill); err != nil && !errdefs.IsNotFound(err) {
			return fmt.Errorf("failed to delete task: %w", err)
		}
	} else if !errdefs.IsNotFound(err) {
		return fmt.Errorf("failed to load task: %w", err)
	}
	return w.startTask(nsCtx, log, ctr)
}
