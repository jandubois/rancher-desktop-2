// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package terminal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"
)

// sizeQueue sends one size, then ends.
type sizeQueue struct {
	sizes chan *remotecommand.TerminalSize
}

func (q *sizeQueue) Next() *remotecommand.TerminalSize {
	return <-q.sizes
}

// runExecutor runs client-go's WebSocket executor against the harness, and
// returns a channel that receives its error once the session ends.
func runExecutor(t *testing.T, h *harness, stdout io.Writer) <-chan error {
	t.Helper()
	executor, err := remotecommand.NewWebSocketExecutor(&rest.Config{Host: h.server.URL}, "GET", h.server.URL)
	assert.NilError(t, err)
	queue := &sizeQueue{sizes: make(chan *remotecommand.TerminalSize, 2)}
	queue.sizes <- &remotecommand.TerminalSize{Width: 100, Height: 30}
	stdinR, stdinW := io.Pipe()
	t.Cleanup(func() {
		_ = stdinW.Close()
		close(queue.sizes)
	})
	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result <- executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:             stdinR,
			Stdout:            stdout,
			Tty:               true,
			TerminalSizeQueue: queue,
		})
	}()
	return result
}

func waitResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(10 * time.Second):
		assert.Assert(t, false, "the executor didn't return")
		return nil
	}
}

func TestClientGoExecutor(t *testing.T) {
	tests := []struct {
		name  string
		code  int
		check func(t *testing.T, err error)
	}{
		{"exit 0", 0, func(t *testing.T, err error) { assert.NilError(t, err) }},
		{"exit 3", 3, func(t *testing.T, err error) {
			var exitErr exec.CodeExitError
			assert.Assert(t, errors.As(err, &exitErr), "error %v", err)
			assert.Equal(t, 3, exitErr.Code)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			var stdout bytes.Buffer
			result := runExecutor(t, h, &stdout)
			s := h.nextStart()
			assert.Equal(t, termSize{100, 30}, s.size)
			s.shell.output(t, "hello")
			s.shell.exitWith(tt.code)
			tt.check(t, waitResult(t, result))
			assert.Equal(t, "hello", stdout.String())
		})
	}
}

func TestClientGoExecutorFailures(t *testing.T) {
	t.Run("start error", func(t *testing.T) {
		h := newHarness(t)
		h.startErr = errors.New("container isn't running")
		err := waitResult(t, runExecutor(t, h, io.Discard))
		assert.Error(t, err, "container isn't running")
	})
	t.Run("take-over", func(t *testing.T) {
		h := newHarness(t)
		result := runExecutor(t, h, io.Discard)
		h.nextStart()
		h.dial()
		err := waitResult(t, result)
		assert.Error(t, err, conflictStatus().Message)
	})
}
