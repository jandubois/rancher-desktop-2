// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"

	cliexit "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/cli/exit"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/cli/help"
	service "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/service/cmd"
)

func newTerminalCommand() *cobra.Command {
	var size string
	var detachAfter time.Duration
	command := &cobra.Command{
		Use:   "terminal PATH",
		Short: "Attach to a terminal session",
		Long: help.Doc(`
			Attach to a terminal session on a passthrough endpoint of the
			API server, such as /passthrough/engine/exec/CONTAINER or
			/passthrough/limavm/shell/NAMESPACE/NAME. This command exists for
			tests.

			The command copies its input to the terminal and the terminal's
			output to its own output, and exits with the shell's exit status.
			It doesn't start the control plane.
		`),
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			termSize, err := parseTerminalSize(size)
			if err != nil {
				return err
			}
			return terminalAction(cmd.Context(), args[0], termSize, detachAfter)
		},
	}
	command.Flags().StringVar(&size, "size", "80x24", "Terminal size as COLSxROWS")
	command.Flags().DurationVar(&detachAfter, "detach-after", 0,
		"Detach this long after the input ends, leaving the shell running; 0 waits for the shell to exit")
	return command
}

// parseTerminalSize parses a size given as COLSxROWS.
func parseTerminalSize(value string) (remotecommand.TerminalSize, error) {
	cols, rows, ok := strings.Cut(value, "x")
	width, widthErr := strconv.ParseUint(cols, 10, 16)
	height, heightErr := strconv.ParseUint(rows, 10, 16)
	if !ok || widthErr != nil || heightErr != nil || width == 0 || height == 0 {
		return remotecommand.TerminalSize{}, fmt.Errorf("invalid size %q: want COLSxROWS, such as 80x24", value)
	}
	return remotecommand.TerminalSize{Width: uint16(width), Height: uint16(height)}, nil
}

func terminalAction(ctx context.Context, path string, size remotecommand.TerminalSize, detachAfter time.Duration) error {
	if !strings.HasPrefix(path, "/passthrough/") {
		return fmt.Errorf("path %q doesn't start with /passthrough/", path)
	}
	config, err := service.GetKubeRestConfig()
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	executor, err := remotecommand.NewWebSocketExecutor(config, "GET", strings.TrimSuffix(config.Host, "/")+path)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stdin := &eofReader{reader: os.Stdin, eof: make(chan struct{})}
	var detached bool
	var detachedMu sync.Mutex
	if detachAfter > 0 {
		go func() {
			select {
			case <-stdin.eof:
			case <-ctx.Done():
				return
			}
			select {
			case <-time.After(detachAfter):
				detachedMu.Lock()
				detached = true
				detachedMu.Unlock()
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            os.Stdout,
		Tty:               true,
		TerminalSizeQueue: &onceSizeQueue{ctx: ctx, size: &size},
	})
	detachedMu.Lock()
	defer detachedMu.Unlock()
	if detached {
		// Closing the connection leaves the shell running in rdd.
		return nil
	}
	if exitErr, ok := errors.AsType[utilexec.CodeExitError](err); ok {
		// The shell's output has already said what went wrong.
		return &cliexit.Error{Code: exitErr.Code}
	}
	return err
}

// eofReader closes eof once its reader reaches the end of the input.
type eofReader struct {
	reader io.Reader
	eof    chan struct{}
	once   sync.Once
}

func (r *eofReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if errors.Is(err, io.EOF) {
		r.once.Do(func() { close(r.eof) })
	}
	return n, err
}

// onceSizeQueue reports one terminal size, then blocks until ctx ends.
type onceSizeQueue struct {
	ctx  context.Context
	size *remotecommand.TerminalSize
}

func (q *onceSizeQueue) Next() *remotecommand.TerminalSize {
	if size := q.size; size != nil {
		q.size = nil
		return size
	}
	<-q.ctx.Done()
	return nil
}
