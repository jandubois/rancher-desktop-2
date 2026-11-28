// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package guestexec

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lima-vm/lima/v2/pkg/limatype"
	"github.com/lima-vm/lima/v2/pkg/limatype/filenames"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"gotest.tools/v3/assert"
)

func TestHostKeyAlgorithms(t *testing.T) {
	ed, _, err := ed25519.GenerateKey(rand.Reader)
	assert.NilError(t, err)
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NilError(t, err)
	rs, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NilError(t, err)
	tests := []struct {
		name string
		key  any
		want []string
	}{
		{"ed25519", ed, []string{ssh.KeyAlgoED25519}},
		{"ecdsa", &ec.PublicKey, []string{ssh.KeyAlgoECDSA256}},
		{"rsa", &rs.PublicKey, []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ssh.NewPublicKey(tt.key)
			assert.NilError(t, err)
			assert.DeepEqual(t, hostKeyAlgorithms(key), tt.want)
		})
	}
}

type ptyRequest struct {
	Term                         string
	Columns, Rows, Width, Height uint32
	Modes                        string
}

type windowChange struct {
	Columns, Rows, Width, Height uint32
}

// testServer is an SSH server that doesn't run commands. On exec it prints
// "ran <command>", waits for one line of input, and exits with exitStatus.
// With dropConnection set, it closes the connection after the output instead.
type testServer struct {
	addr             *net.TCPAddr
	exitStatus       atomic.Uint32
	ptys             chan ptyRequest
	windowChanges    chan windowChange
	ignoreKeepalives atomic.Bool
	dropConnection   atomic.Bool
	dialed           chan struct{}
}

// newTestServer starts a server with an ECDSA and an ed25519 host key, so a
// client that asks for ECDSA first gets a key that isn't pinned. It returns the
// server with an instance whose known_hosts pins pinnedKey, or the server's
// ed25519 key when pinnedKey is nil. It points LIMA_HOME at a directory with
// the user key that the server accepts.
func newTestServer(t *testing.T, pinnedKey ssh.PublicKey) (*testServer, *limatype.Instance) {
	t.Helper()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	assert.NilError(t, err)
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	assert.NilError(t, err)
	if pinnedKey == nil {
		pinnedKey = hostSigner.PublicKey()
	}

	limaHome := t.TempDir()
	t.Setenv("LIMA_HOME", limaHome)
	_, userPriv, err := ed25519.GenerateKey(rand.Reader)
	assert.NilError(t, err)
	block, err := ssh.MarshalPrivateKey(userPriv, "")
	assert.NilError(t, err)
	assert.NilError(t, os.MkdirAll(filepath.Join(limaHome, filenames.ConfigDir), 0o700))
	assert.NilError(t, os.WriteFile(filepath.Join(limaHome, filenames.ConfigDir, filenames.UserPrivateKey), pem.EncodeToMemory(block), 0o600))
	userPub, err := ssh.NewPublicKey(userPriv.Public())
	assert.NilError(t, err)

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() == "tester" && bytes.Equal(key.Marshal(), userPub.Marshal()) {
				return nil, nil
			}
			return nil, io.ErrUnexpectedEOF
		},
	}
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NilError(t, err)
	ecdsaSigner, err := ssh.NewSignerFromKey(ecdsaKey)
	assert.NilError(t, err)
	config.AddHostKey(ecdsaSigner)
	config.AddHostKey(hostSigner)

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	assert.NilError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	srv := &testServer{
		addr:          listener.Addr().(*net.TCPAddr),
		ptys:          make(chan ptyRequest, 1),
		windowChanges: make(chan windowChange, 1),
		dialed:        make(chan struct{}, 1),
	}
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		srv.dialed <- struct{}{}
		srv.serve(t, conn, config)
	}()

	instDir := t.TempDir()
	line := knownhosts.Line([]string{"lima-test", srv.addr.String()}, pinnedKey) + "\n"
	assert.NilError(t, os.WriteFile(filepath.Join(instDir, filenames.SSHKnownHosts), []byte(line), 0o600))
	user := "tester"
	inst := &limatype.Instance{
		Name:         "test",
		Dir:          instDir,
		SSHAddress:   "127.0.0.1",
		SSHLocalPort: srv.addr.Port,
		Config:       &limatype.LimaYAML{User: limatype.User{Name: &user}},
	}
	return srv, inst
}

func (srv *testServer) serve(t *testing.T, conn net.Conn, config *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	t.Cleanup(func() { _ = sconn.Close() })
	go func() {
		for req := range reqs {
			if !srv.ignoreKeepalives.Load() {
				_ = req.Reply(true, nil)
			}
		}
	}()
	for newChan := range chans {
		ch, chReqs, err := newChan.Accept()
		if err != nil {
			return
		}
		go srv.handleSession(sconn, ch, chReqs)
	}
}

func (srv *testServer) handleSession(sconn *ssh.ServerConn, ch ssh.Channel, reqs <-chan *ssh.Request) {
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			var p ptyRequest
			_ = ssh.Unmarshal(req.Payload, &p)
			srv.ptys <- p
			_ = req.Reply(true, nil)
		case "window-change":
			var w windowChange
			_ = ssh.Unmarshal(req.Payload, &w)
			srv.windowChanges <- w
		case "exec":
			var e struct{ Command string }
			_ = ssh.Unmarshal(req.Payload, &e)
			_ = req.Reply(true, nil)
			go func() {
				_, _ = io.WriteString(ch, "ran "+e.Command+"\r\n")
				if srv.dropConnection.Load() {
					_ = sconn.Close()
					return
				}
				if _, err := bufio.NewReader(ch).ReadString('\n'); err != nil {
					return
				}
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{srv.exitStatus.Load()}))
				_ = ch.Close()
			}()
		default:
			_ = req.Reply(false, nil)
		}
	}
}

func TestPTYSession(t *testing.T) {
	srv, inst := newTestServer(t, nil)
	srv.exitStatus.Store(42)
	s, err := StartPTYSession(t.Context(), inst, "my-shell", 80, 24)
	assert.NilError(t, err)
	defer s.Close()

	pty := <-srv.ptys
	assert.Equal(t, pty.Term, "xterm-256color")
	assert.Equal(t, [2]uint32{pty.Columns, pty.Rows}, [2]uint32{80, 24})

	line, err := bufio.NewReader(s).ReadString('\n')
	assert.NilError(t, err)
	assert.Equal(t, line, "ran my-shell\r\n")

	assert.NilError(t, s.Resize(132, 43))
	wc := <-srv.windowChanges
	assert.Equal(t, [2]uint32{wc.Columns, wc.Rows}, [2]uint32{132, 43})

	_, err = io.WriteString(s, "exit\n")
	assert.NilError(t, err)
	status, err := s.Wait()
	assert.NilError(t, err)
	assert.Equal(t, status, 42)

	assert.NilError(t, s.Close())
	assert.NilError(t, s.Close())
}

func TestPTYSessionRefusesOtherHostKey(t *testing.T) {
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	assert.NilError(t, err)
	otherKey, err := ssh.NewPublicKey(otherPriv.Public())
	assert.NilError(t, err)
	_, inst := newTestServer(t, otherKey)
	_, err = StartPTYSession(t.Context(), inst, "my-shell", 80, 24)
	assert.ErrorContains(t, err, "host key mismatch")
}

func TestPTYSessionRefusesUnpinnedInstance(t *testing.T) {
	srv, inst := newTestServer(t, nil)
	assert.NilError(t, os.WriteFile(filepath.Join(inst.Dir, filenames.SSHKnownHosts), nil, 0o600))
	_, err := StartPTYSession(t.Context(), inst, "my-shell", 80, 24)
	assert.ErrorContains(t, err, "no pinned SSH host key")
	select {
	case <-srv.dialed:
		assert.Assert(t, false, "connected to the server without a pinned host key")
	default:
	}
}

func TestPTYSessionClosesUnansweredKeepalives(t *testing.T) {
	saved := keepaliveInterval
	keepaliveInterval = 50 * time.Millisecond
	t.Cleanup(func() { keepaliveInterval = saved })

	srv, inst := newTestServer(t, nil)
	srv.ignoreKeepalives.Store(true)
	s, err := StartPTYSession(t.Context(), inst, "my-shell", 80, 24)
	assert.NilError(t, err)
	defer s.Close()

	waited := make(chan error, 1)
	go func() {
		_, err := s.Wait()
		waited <- err
	}()
	select {
	case err := <-waited:
		assert.Assert(t, errors.Is(err, io.ErrUnexpectedEOF), "error %v", err)
	case <-time.After(5 * time.Second):
		assert.Assert(t, false, "the session outlived unanswered keepalives")
	}
}

func TestPTYSessionReportsLostConnection(t *testing.T) {
	srv, inst := newTestServer(t, nil)
	srv.dropConnection.Store(true)
	s, err := StartPTYSession(t.Context(), inst, "my-shell", 80, 24)
	assert.NilError(t, err)
	defer s.Close()

	output, err := io.ReadAll(s)
	assert.NilError(t, err)
	assert.Equal(t, string(output), "ran my-shell\r\n")
	_, err = s.Wait()
	assert.Assert(t, errors.Is(err, io.ErrUnexpectedEOF), "error %v", err)
}
