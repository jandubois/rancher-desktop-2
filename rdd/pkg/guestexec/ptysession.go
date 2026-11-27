// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package guestexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/lima-vm/lima/v2/pkg/limatype"
	"github.com/lima-vm/lima/v2/pkg/limatype/dirnames"
	"github.com/lima-vm/lima/v2/pkg/limatype/filenames"
	"github.com/lima-vm/lima/v2/pkg/sshutil"
	"golang.org/x/crypto/ssh"
)

const (
	ptyConnectTimeout = 30 * time.Second
	ptyTerm           = "xterm-256color"
	keepaliveCountMax = 3
)

// keepaliveInterval is how often a PTYSession checks that the guest still
// answers. Tests shorten it.
var keepaliveInterval = 30 * time.Second

// PTYSession is one command running under a pty in a Lima VM, over its own
// SSH connection. Read returns the command's output, which includes its
// stderr; Write sends input.
type PTYSession struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader

	done      chan struct{}
	closeOnce sync.Once
}

// StartPTYSession connects to inst as the instance's user and runs command
// under a pty of the given size.
func StartPTYSession(ctx context.Context, inst *limatype.Instance, command string, columns, rows int) (*PTYSession, error) {
	client, err := dialInstance(ctx, inst)
	if err != nil {
		return nil, err
	}
	s, err := startPTYSession(client, command, columns, rows)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	go s.keepalive()
	return s, nil
}

func startPTYSession(client *ssh.Client, command string, columns, rows int) (*PTYSession, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := session.RequestPty(ptyTerm, rows, columns, ssh.TerminalModes{}); err != nil {
		return nil, fmt.Errorf("failed to request a pty: %w", err)
	}
	if err := session.Start(command); err != nil {
		return nil, fmt.Errorf("failed to start %q: %w", command, err)
	}
	return &PTYSession{
		client:  client,
		session: session,
		stdin:   stdin,
		stdout:  stdout,
		done:    make(chan struct{}),
	}, nil
}

// dialInstance opens an SSH connection to inst, accepting only the host key
// that Lima pinned for it.
func dialInstance(ctx context.Context, inst *limatype.Instance) (*ssh.Client, error) {
	hostKey, err := sshutil.ReadPinnedHostKey(inst.Dir)
	if err != nil {
		return nil, err
	}
	if hostKey == nil {
		return nil, fmt.Errorf("instance %q has no pinned SSH host key yet", inst.Name)
	}
	signer, err := userKeySigner()
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:              *inst.Config.User.Name,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback:   ssh.FixedHostKey(hostKey),
		HostKeyAlgorithms: hostKeyAlgorithms(hostKey),
	}

	ctx, cancel := context.WithTimeout(ctx, ptyConnectTimeout)
	defer cancel()
	addr := net.JoinHostPort(inst.SSHAddress, strconv.Itoa(inst.SSHLocalPort))
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	// Closing the connection aborts a handshake that outlives ctx.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if !stop() {
		if err == nil {
			_ = c.Close()
		}
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, ctx.Err())
	}
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// hostKeyAlgorithms returns the host key algorithms that match key. The guest
// has host keys of several types, and Lima pins one of them, the ed25519 key
// when the guest has one. Without this list, x/crypto asks for ECDSA first, and
// the guest would present a key that doesn't match the pinned one.
//
// An RSA key's type, ssh-rsa, also names a signature algorithm that uses SHA-1.
// OpenSSH turns that algorithm off by default, so an RSA key gets the SHA-2
// algorithms instead.
func hostKeyAlgorithms(key ssh.PublicKey) []string {
	if key.Type() == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	}
	return []string{key.Type()}
}

// userKeySigner reads Lima's user private key from $LIMA_HOME/_config.
func userKeySigner() (ssh.Signer, error) {
	configDir, err := dirnames.LimaConfigDir()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(configDir, filenames.UserPrivateKey))
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(b)
}

// keepalive closes the connection when keepaliveCountMax requests in a row go
// unanswered, as Command configures the ssh binary to do. Otherwise a session
// whose connection hangs would never end.
func (s *PTYSession) keepalive() {
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()
	missed := 0
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}
		if s.ping() {
			missed = 0
			continue
		}
		missed++
		if missed >= keepaliveCountMax {
			_ = s.client.Close()
			return
		}
	}
}

// ping reports whether the guest answers a keepalive request within
// keepaliveInterval. A request that is still pending at that point counts as
// unanswered. The goroutine that sent it returns when the reply arrives or the
// connection closes.
func (s *PTYSession) ping() bool {
	answered := make(chan bool, 1)
	go func() {
		_, _, err := s.client.SendRequest("keepalive@openssh.com", true, nil)
		answered <- err == nil
	}()
	select {
	case ok := <-answered:
		return ok
	case <-time.After(keepaliveInterval):
		return false
	case <-s.done:
		return true
	}
}

// Read reads the command's output.
func (s *PTYSession) Read(p []byte) (int, error) {
	return s.stdout.Read(p)
}

// Write sends input to the command.
func (s *PTYSession) Write(p []byte) (int, error) {
	return s.stdin.Write(p)
}

// Resize changes the size of the pty.
func (s *PTYSession) Resize(columns, rows int) error {
	return s.session.WindowChange(rows, columns)
}

// Wait waits for the command to end and returns its exit status. It returns
// an error when the command ends without an exit status, for example on a
// signal. When the connection closes before the guest sends the exit status
// or signal, the error wraps io.ErrUnexpectedEOF.
func (s *PTYSession) Wait() (int, error) {
	err := s.session.Wait()
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) && exitErr.Signal() == "" {
		return exitErr.ExitStatus(), nil
	}
	var missingErr *ssh.ExitMissingError
	if errors.As(err, &missingErr) {
		return 0, fmt.Errorf("%w: %w", io.ErrUnexpectedEOF, err)
	}
	if err != nil {
		return 0, err
	}
	return 0, nil
}

// Close ends the session and its connection. It is safe to call more than
// once.
func (s *PTYSession) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.session.Close()
		err = s.client.Close()
	})
	return err
}
