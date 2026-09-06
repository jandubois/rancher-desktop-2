// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"time"

	apievents "github.com/containerd/containerd/api/events"
	containerdclient "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/events"
	typeurl "github.com/containerd/typeurl/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	containersv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/instance"
)

var _ engine = (*containerdWatcher)(nil)

// containerdWatcher manages a containerd client connection and event stream.
// It performs a full sync on connect and then watches for incremental changes.
type containerdWatcher struct {
	mirrorClient

	cli *containerdclient.Client

	cancel context.CancelFunc
	done   chan struct{}

	// enqueue is used to trigger reconciliation in the engine reconciler.
	enqueue func()
}

// newContainerdWatcher creates a containerd client, performs a full sync, and
// starts the event stream watcher goroutine.
func newContainerdWatcher(ctx context.Context, k8s client.Client, apiNamespace string, enqueue func()) (*containerdWatcher, error) {
	cli, err := containerdclient.New(instance.ContainerdSocket())
	if err != nil {
		return nil, fmt.Errorf("failed to create containerd client: %w", err)
	}

	// Verify the connection.
	servingCtx, servingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer servingCancel()
	serving, err := cli.IsServing(servingCtx)
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("failed to reach containerd: %w", err)
	}
	if !serving {
		cli.Close()
		return nil, errors.New("containerd is not serving")
	}

	watchCtx, watchCancel := context.WithCancel(ctx)

	w := &containerdWatcher{
		mirrorClient: mirrorClient{k8s: k8s, apiNamespace: apiNamespace},
		cli:          cli,
		cancel:       watchCancel,
		done:         make(chan struct{}),
		enqueue:      enqueue,
	}

	// Subscribe before the initial fullSync, the opposite order from the
	// Docker watcher. containerd's event service has no Since/replay, so the
	// subscription must already be open before the snapshot is taken;
	// otherwise events fired during fullSync are lost. Events that race the
	// snapshot are re-applied afterwards, and handleEvent is idempotent.
	eventCh, errCh := cli.Subscribe(watchCtx,
		`topic~="^/containers/"`, `topic~="^/tasks/"`, `topic~="^/namespaces/"`)

	if err := w.fullSync(watchCtx); err != nil {
		watchCancel()
		cli.Close()
		return nil, fmt.Errorf("failed to perform initial sync: %w", err)
	}

	go w.run(watchCtx, eventCh, errCh)

	return w, nil
}

// stop cancels the watcher goroutine and waits for it to finish.
// run's deferred cleanup closes the containerd client; stop only signals
// the goroutine and blocks until it exits.
func (w *containerdWatcher) stop() {
	w.cancel()
	<-w.done
}

// alive returns true if the watcher goroutine is still running.
func (w *containerdWatcher) alive() bool {
	select {
	case <-w.done:
		return false
	default:
		return true
	}
}

// run is the main watcher goroutine.
//
// run owns the containerd client's lifetime: it closes cli before
// returning, so a caller that observes alive()==false can drop its
// reference to the watcher without a separate cleanup step.
func (w *containerdWatcher) run(ctx context.Context, eventCh <-chan *events.Envelope, errCh <-chan error) {
	log := logf.FromContext(ctx).WithName("containerd-watcher")
	// Defers fire LIFO, giving this exit sequence:
	//
	//   1. close(w.done): alive() now returns false
	//   2. w.cli.Close(): containerd client released
	//   3. w.enqueue(): reconciler wakes and sees !alive()
	//
	// The order matters: if w.enqueue ran before w.done closed,
	// the reconciler could wake, see alive()==true on the about-to-exit
	// goroutine, and skip the restart. Closing cli between done and
	// enqueue means the reconciler observes the dead watcher only
	// after its client has been released.
	defer w.enqueue()
	defer w.cli.Close()
	defer close(w.done)
	// Keep a bad event shape from crashing the whole app-controller.
	defer func() {
		if r := recover(); r != nil {
			log.Error(nil, "panic in containerd watcher goroutine",
				"recovered", r, "stack", string(debug.Stack()))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.Info("Containerd watcher stopping")
			return
		case err := <-errCh:
			if ctx.Err() != nil {
				return
			}
			// The reconciler restarts the watcher via the deferred
			// enqueue.
			log.Error(err, "Containerd event stream error")
			return
		case e, ok := <-eventCh:
			if !ok {
				log.Info("Containerd event stream closed")
				return
			}
			// Transient handleEvent errors (API error, SSA conflict past
			// its internal retry) are logged and dropped. Container events
			// self-heal on the next state change; a dropped apply leaves the
			// mirror stale until the next full resync.
			if err := w.handleEvent(ctx, e); err != nil {
				log.Error(err, "Failed to handle containerd event",
					"topic", e.Topic, "namespace", e.Namespace)
			}
		}
	}
}

// handleEvent processes a single containerd event.
func (w *containerdWatcher) handleEvent(ctx context.Context, e *events.Envelope) error {
	log := logf.FromContext(ctx).WithName("containerd-watcher")

	decoded, err := typeurl.UnmarshalAny(e.Event)
	if err != nil {
		return fmt.Errorf("failed to unmarshal containerd event: %w", err)
	}

	switch ev := decoded.(type) {
	case *apievents.NamespaceCreate:
		// ev.Name is where the event type carries the subject. containerd
		// stamps the envelope namespace with the same name before publishing,
		// so e.Namespace happens to agree, but only on the namespace topics.
		log.V(1).Info("Namespace created", "namespace", ev.Name)
		return w.applyNamespace(ctx, ev.Name)
	case *apievents.NamespaceDelete:
		log.V(1).Info("Namespace deleted", "namespace", ev.Name)
		return w.removeNamespace(ctx, ev.Name)
	case *apievents.NamespaceUpdate:
		// The mirror carries nothing but the name, so a label change on the
		// containerd namespace has nothing to propagate.
		return nil
	default:
		return nil
	}
}

// processContainerAction records the requested container action as failed,
// since action dispatch is not implemented yet. Consuming the annotation
// keeps the reconciler from retrying forever.
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

	lastAction := &containersv1alpha1.ContainerLastAction{
		Action:      action,
		ObservedAt:  observedAt,
		CompletedAt: metav1.Now(),
		State:       containersv1alpha1.ContainerActionFailed,
		Error:       "container actions are not supported with the containerd engine yet",
	}

	latest, err := w.patchContainerLastAction(ctx, c.Name, lastAction)
	if err != nil {
		return fmt.Errorf("failed to patch lastAction for %s: %w", c.Name, err)
	}
	if latest == nil {
		// Mirror was deleted between the read and the status patch; nothing
		// left to clean up.
		return nil
	}
	if err := w.removeActionAnnotation(ctx, latest, raw); err != nil {
		return fmt.Errorf("failed to remove action annotation for %s: %w", c.Name, err)
	}
	return nil
}

// hasTTY is not wired for containerd. nerdctl owns the container log files
// inside the VM, and containerd itself exposes no log API.
func (w *containerdWatcher) hasTTY(_ context.Context, _ *containersv1alpha1.Container) (bool, error) {
	return false, errLogsNotSupported
}

// getLogs is not wired for containerd. nerdctl owns the container log files
// inside the VM, and containerd itself exposes no log API.
func (w *containerdWatcher) getLogs(_ context.Context, _ *containersv1alpha1.Container, _ ...engineLogOptions) (io.ReadCloser, error) {
	return nil, errLogsNotSupported
}

// pullImage is not wired for containerd; an ImagePullRequest fails with this
// error.
func (w *containerdWatcher) pullImage(_ context.Context, _ string, _ func(start, current, total int64, units string), _ func(error)) error {
	return errors.New("image pulls are not supported with the containerd engine")
}

// deleteContainer is unreachable yet. containerd mirrors have no mirror
// finalizer, so no K8s-side delete is forwarded here.
func (w *containerdWatcher) deleteContainer(_ context.Context, _ *containersv1alpha1.Container) error {
	return errors.New("container deletion is not supported with the containerd engine yet")
}

// deleteImage is unreachable yet. containerd Image mirrors do not exist.
func (w *containerdWatcher) deleteImage(_ context.Context, _ *containersv1alpha1.Image) error {
	return errors.New("image deletion is not supported with the containerd engine yet")
}

// deleteVolume returns nil, because containerd has no native volume concept.
func (w *containerdWatcher) deleteVolume(_ context.Context, _ *containersv1alpha1.Volume) error {
	return nil
}

// fullSync lists containerd namespaces and creates their mirror resources,
// pruning stale ones. Container and image mirrors are not implemented yet;
// containerd has no volumes.
func (w *containerdWatcher) fullSync(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("containerd-watcher")
	log.Info("Starting full sync")

	var errs []error

	if err := w.syncNamespaces(ctx); err != nil {
		errs = append(errs, fmt.Errorf("failed to sync namespaces: %w", err))
	}

	log.Info("Full sync complete", "errors", len(errs))
	return errors.Join(errs...)
}
