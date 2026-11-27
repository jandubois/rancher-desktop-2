// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package terminal

import (
	"context"
	"encoding/json"
	"errors"
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
	"k8s.io/apimachinery/pkg/util/remotecommand"
)

// fakeShell is a Shell whose output the test writes and whose input and
// resizes the test reads.
type fakeShell struct {
	outR      *io.PipeReader
	outW      *io.PipeWriter
	input     chan string
	resizes   chan termSize
	exit      chan int
	closeOnce sync.Once
}

func newFakeShell() *fakeShell {
	r, w := io.Pipe()
	return &fakeShell{
		outR:    r,
		outW:    w,
		input:   make(chan string, 100),
		resizes: make(chan termSize, 100),
		exit:    make(chan int, 1),
	}
}

func (f *fakeShell) Read(p []byte) (int, error) { return f.outR.Read(p) }
func (f *fakeShell) Write(p []byte) (int, error) {
	f.input <- string(p)
	return len(p), nil
}

func (f *fakeShell) Resize(columns, rows int) error {
	f.resizes <- termSize{columns, rows}
	return nil
}
func (f *fakeShell) Wait() (int, error) { return <-f.exit, nil }
func (f *fakeShell) Close() error {
	f.closeOnce.Do(func() { _ = f.outR.Close() })
	return nil
}

func (f *fakeShell) output(t *testing.T, s string) {
	t.Helper()
	_, err := f.outW.Write([]byte(s))
	assert.NilError(t, err)
}

func (f *fakeShell) exitWith(code int) {
	f.exit <- code
	_ = f.outW.Close()
}

type started struct {
	size  termSize
	shell *fakeShell
}

type harness struct {
	t        *testing.T
	server   *httptest.Server
	starts   chan started
	startErr error
	detached chan struct{} // signalled when Attach returns
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, starts: make(chan started, 10), detached: make(chan struct{}, 10)}
	registry := NewRegistry()
	start := func(_ context.Context, columns, rows int) (Shell, error) {
		if h.startErr != nil {
			return nil, h.startErr
		}
		shell := newFakeShell()
		h.starts <- started{termSize{columns, rows}, shell}
		return shell, nil
	}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		registry.Attach(w, req, "key", start)
		select {
		case h.detached <- struct{}{}:
		default:
		}
	}))
	t.Cleanup(h.server.Close)
	return h
}

func (h *harness) dial() *websocket.Conn {
	h.t.Helper()
	dialer := websocket.Dialer{Subprotocols: []string{remotecommand.StreamProtocolV5Name}}
	conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(h.server.URL, "http"), nil)
	assert.NilError(h.t, err)
	_ = resp.Body.Close()
	h.t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func (h *harness) waitDetached() {
	h.t.Helper()
	select {
	case <-h.detached:
	case <-time.After(5 * time.Second):
		assert.Assert(h.t, false, "Attach didn't return")
	}
}

func (h *harness) nextStart() started {
	h.t.Helper()
	select {
	case s := <-h.starts:
		return s
	case <-time.After(5 * time.Second):
		assert.Assert(h.t, false, "shell didn't start")
		return started{}
	}
}

func send(t *testing.T, conn *websocket.Conn, channel byte, payload string) {
	t.Helper()
	assert.NilError(t, conn.WriteMessage(websocket.BinaryMessage, frame(channel, []byte(payload))))
}

func sendSize(t *testing.T, conn *websocket.Conn, width, height int) {
	t.Helper()
	data, err := json.Marshal(termSize{width, height})
	assert.NilError(t, err)
	send(t, conn, remotecommand.StreamResize, string(data))
}

// receive returns the next message's channel and payload.
func receive(t *testing.T, conn *websocket.Conn) (channel byte, payload string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	messageType, data, err := conn.ReadMessage()
	assert.NilError(t, err)
	assert.Equal(t, websocket.BinaryMessage, messageType)
	assert.Assert(t, len(data) > 0)
	return data[0], string(data[1:])
}

func receiveOutput(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	channel, payload := receive(t, conn)
	assert.Equal(t, byte(remotecommand.StreamStdOut), channel, "payload %q", payload)
	return payload
}

func receiveStatus(t *testing.T, conn *websocket.Conn) metav1.Status {
	t.Helper()
	channel, payload := receive(t, conn)
	assert.Equal(t, byte(remotecommand.StreamErr), channel, "payload %q", payload)
	var status metav1.Status
	assert.NilError(t, json.Unmarshal([]byte(payload), &status))
	return status
}

func receiveClose(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_, _, err := conn.ReadMessage()
	var closeErr *websocket.CloseError
	assert.Assert(t, errors.As(err, &closeErr), "error %v", err)
	assert.Equal(t, websocket.CloseNormalClosure, closeErr.Code)
}

// receiveEnd reads the status and then the close frame.
func receiveEnd(t *testing.T, conn *websocket.Conn) metav1.Status {
	t.Helper()
	status := receiveStatus(t, conn)
	receiveClose(t, conn)
	return status
}

func receiveInput(t *testing.T, shell *fakeShell) string {
	t.Helper()
	select {
	case s := <-shell.input:
		return s
	case <-time.After(5 * time.Second):
		assert.Assert(t, false, "the shell didn't get input")
		return ""
	}
}

func receiveResize(t *testing.T, shell *fakeShell) termSize {
	t.Helper()
	select {
	case s := <-shell.resizes:
		return s
	case <-time.After(5 * time.Second):
		assert.Assert(t, false, "the shell wasn't resized")
		return termSize{}
	}
}

func setVar[T any](t *testing.T, v *T, value T) {
	old := *v
	*v = value
	t.Cleanup(func() { *v = old })
}

func TestNewSessionStartsAtFirstSize(t *testing.T) {
	h := newHarness(t)
	conn := h.dial()
	assert.Equal(t, receiveOutput(t, conn), "", "replay")
	send(t, conn, remotecommand.StreamStdIn, "one")
	send(t, conn, remotecommand.StreamStdIn, "two")
	sendSize(t, conn, 120, 40)
	s := h.nextStart()
	assert.Equal(t, termSize{120, 40}, s.size)
	assert.Equal(t, "one", receiveInput(t, s.shell))
	assert.Equal(t, "two", receiveInput(t, s.shell))
	assert.Equal(t, len(s.shell.resizes), 0)
}

func TestNewSessionStartsAtDefaultSize(t *testing.T) {
	setVar(t, &startTimeout, 10*time.Millisecond)
	h := newHarness(t)
	conn := h.dial()
	receiveOutput(t, conn)
	assert.Equal(t, termSize{80, 24}, h.nextStart().size)
}

func TestReattach(t *testing.T) {
	setVar(t, &nudgeDelay, 10*time.Millisecond)
	h := newHarness(t)
	first := h.dial()
	receiveOutput(t, first)
	sendSize(t, first, 100, 30)
	s := h.nextStart()
	s.shell.output(t, "earlier")
	assert.Equal(t, "earlier", receiveOutput(t, first))
	assert.NilError(t, first.Close())

	// The dial can return before the server attaches second, so " later"
	// may reach the buffer before the attach and arrive in the replay.
	// Either way the replay comes first, and second gets every byte once.
	second := h.dial()
	s.shell.output(t, " later")
	got := receiveOutput(t, second)
	assert.Assert(t, strings.HasPrefix(got, "earlier"), "replay %q", got)
	for len(got) < len("earlier later") {
		got += receiveOutput(t, second)
	}
	assert.Equal(t, "earlier later", got)

	sendSize(t, second, 90, 20)
	assert.Equal(t, termSize{90, 19}, receiveResize(t, s.shell))
	assert.Equal(t, termSize{90, 20}, receiveResize(t, s.shell))
}

func TestTakeOver(t *testing.T) {
	h := newHarness(t)
	first := h.dial()
	receiveOutput(t, first)
	sendSize(t, first, 80, 24)
	s := h.nextStart()

	second := h.dial()
	status := receiveStatus(t, first)
	assert.Equal(t, metav1.StatusReasonConflict, status.Reason)
	assert.Equal(t, metav1.StatusFailure, status.Status)
	// The server reads first's messages until first answers the close frame.
	// The session drops this input, so the shell's first input is "from second".
	send(t, first, remotecommand.StreamStdIn, "from first")
	receiveClose(t, first)
	h.waitDetached()
	receiveOutput(t, second)

	send(t, second, remotecommand.StreamStdIn, "from second")
	assert.Equal(t, "from second", receiveInput(t, s.shell))
}

func TestSessionEnd(t *testing.T) {
	tests := []struct {
		name     string
		startErr error
		code     int
		want     metav1.Status
	}{
		{"exit 0", nil, 0, *exitStatus(0, nil)},
		{"exit 3", nil, 3, *exitStatus(3, nil)},
		{"start error", errors.New("container isn't running"), 0, *errorStatus(errors.New("container isn't running"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.startErr = tt.startErr
			conn := h.dial()
			receiveOutput(t, conn)
			sendSize(t, conn, 80, 24)
			if tt.startErr == nil {
				h.nextStart().shell.exitWith(tt.code)
			}
			assert.DeepEqual(t, tt.want, receiveEnd(t, conn))

			h.startErr = nil
			next := h.dial()
			assert.Equal(t, receiveOutput(t, next), "", "replay of a new session")
			sendSize(t, next, 80, 24)
			h.nextStart()
		})
	}
}

func TestRequiresSubprotocol(t *testing.T) {
	h := newHarness(t)
	_, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.server.URL, "http"), nil)
	assert.Assert(t, err != nil)
	assert.Assert(t, resp != nil)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
