// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"gotest.tools/v3/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiremotecommand "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	containersv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/terminal"
)

// execShell is a terminal.Shell whose output the test writes.
type execShell struct {
	outR *io.PipeReader
	outW *io.PipeWriter
	exit chan int
}

func newExecShell() *execShell {
	r, w := io.Pipe()
	return &execShell{outR: r, outW: w, exit: make(chan int, 1)}
}

func (s *execShell) Read(p []byte) (int, error)  { return s.outR.Read(p) }
func (s *execShell) Write(p []byte) (int, error) { return len(p), nil }
func (s *execShell) Resize(_, _ int) error       { return nil }
func (s *execShell) Wait() (int, error)          { return <-s.exit, nil }
func (s *execShell) Close() error                { return s.outR.Close() }

// execHarness serves HandleExec and records each startPTY call.
type execHarness struct {
	t        *testing.T
	r        *EngineReconciler
	server   *httptest.Server
	mu       sync.Mutex
	commands []string
	shell    *execShell
	detached chan struct{} // signalled when HandleExec returns
}

func newExecHarness(t *testing.T, e engine, objs ...*containersv1alpha1.Container) *execHarness {
	h := &execHarness{t: t, shell: newExecShell(), detached: make(chan struct{}, 10)}
	var clientObjs []client.Object
	for _, o := range objs {
		clientObjs = append(clientObjs, o)
	}
	h.r = &EngineReconciler{
		Client: newEngineImagePullReconcilerTestClient(t, clientObjs...),
		engine: e,
		shells: terminal.NewRegistry(),
		startPTY: func(_ context.Context, command string, _, _ int) (terminal.Shell, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.commands = append(h.commands, command)
			return h.shell, nil
		},
	}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		h.r.HandleExec(w, req)
		h.detached <- struct{}{}
	}))
	t.Cleanup(h.server.Close)
	return h
}

func (h *execHarness) startedCommands() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.commands...)
}

// dial opens a raw WebSocket to the container's exec endpoint and sends the
// client's size. Unlike client-go's executor, it can detach without logging
// errors.
func (h *execHarness) dial(name string) *websocket.Conn {
	h.t.Helper()
	dialer := websocket.Dialer{Subprotocols: []string{apiremotecommand.StreamProtocolV5Name}}
	conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(h.server.URL, "http")+"/"+name, nil)
	assert.NilError(h.t, err)
	_ = resp.Body.Close()
	h.t.Cleanup(func() { _ = conn.Close() })
	size := append([]byte{apiremotecommand.StreamResize}, `{"Width":100,"Height":30}`...)
	assert.NilError(h.t, conn.WriteMessage(websocket.BinaryMessage, size))
	return conn
}

// attach runs client-go's WebSocket executor on the container's exec
// endpoint, and returns a channel that receives its error once the stream
// ends.
func (h *execHarness) attach(name string, stdout io.Writer) <-chan error {
	h.t.Helper()
	url := h.server.URL + "/" + name
	executor, err := remotecommand.NewWebSocketExecutor(&rest.Config{Host: h.server.URL}, "GET", url)
	assert.NilError(h.t, err)
	sizes := make(chan *remotecommand.TerminalSize, 1)
	sizes <- &remotecommand.TerminalSize{Width: 100, Height: 30}
	stdinR, stdinW := io.Pipe()
	h.t.Cleanup(func() {
		_ = stdinW.Close()
		close(sizes)
	})
	result := make(chan error, 1)
	go func() {
		result <- executor.StreamWithContext(h.t.Context(), remotecommand.StreamOptions{
			Stdin:             stdinR,
			Stdout:            stdout,
			Tty:               true,
			TerminalSizeQueue: sizeQueue(sizes),
		})
	}()
	return result
}

type sizeQueue chan *remotecommand.TerminalSize

func (q sizeQueue) Next() *remotecommand.TerminalSize { return <-q }

// outputWriter sends each write to a channel.
type outputWriter chan string

func (w outputWriter) Write(p []byte) (int, error) {
	w <- string(p)
	return len(p), nil
}

// waitFor fails the test unless ch delivers within the timeout.
func waitFor[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		assert.Assert(t, false, "timed out waiting for %s", what)
		var zero T
		return zero
	}
}

func execTestContainer(status containersv1alpha1.ContainerStatusValue) *containersv1alpha1.Container {
	return &containersv1alpha1.Container{
		ObjectMeta: metav1.ObjectMeta{Namespace: appv1alpha1.AppNamespace, Name: "web"},
		Status: containersv1alpha1.ContainerStatus{
			ID:     "0123abcd",
			Status: status,
		},
	}
}

func TestHandleExecStartErrors(t *testing.T) {
	tests := []struct {
		name    string
		engine  engine
		objs    []*containersv1alpha1.Container
		message string
	}{
		{"engine not running", nil, []*containersv1alpha1.Container{execTestContainer(containersv1alpha1.ContainerStatusRunning)}, "the container engine is not running"},
		{"container not found", &testEngine{}, nil, `container "web" not found`},
		{"container exited", &testEngine{}, []*containersv1alpha1.Container{execTestContainer(containersv1alpha1.ContainerStatusExited)}, `container "web" is not running (status: exited)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newExecHarness(t, tt.engine, tt.objs...)
			err := waitFor(t, h.attach("web", io.Discard), "the executor")
			assert.ErrorContains(t, err, tt.message)
			assert.Equal(t, len(h.startedCommands()), 0)
		})
	}
}

func TestHandleExecReattachWithoutEngine(t *testing.T) {
	h := newExecHarness(t, &testEngine{}, execTestContainer(containersv1alpha1.ContainerStatusRunning))

	first := h.dial("web")
	// The empty replay comes first, then the shell's output.
	go func() { _, _ = h.shell.outW.Write([]byte("hello")) }()
	got := ""
	for !strings.Contains(got, "hello") {
		_, msg, err := first.ReadMessage()
		assert.NilError(t, err)
		assert.Equal(t, msg[0], byte(apiremotecommand.StreamStdOut))
		got += string(msg[1:])
	}
	assert.DeepEqual(t, h.startedCommands(), []string{testShellCommand})
	assert.NilError(t, first.Close())
	waitFor(t, h.detached, "the first detach")

	h.r.engineMu.Lock()
	h.r.engine = nil
	h.r.engineMu.Unlock()

	second := make(outputWriter, 100)
	result := h.attach("web", second)
	assert.Equal(t, waitFor(t, second, "the replay"), "hello")
	h.shell.exit <- 0
	_ = h.shell.outW.Close()
	assert.NilError(t, waitFor(t, result, "the second executor"))
	assert.DeepEqual(t, h.startedCommands(), []string{testShellCommand})
}
