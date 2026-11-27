// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package terminal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/websocket"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/remotecommand"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	// startTimeout is how long a new session waits for the client's size
	// before it starts the shell at the default size.
	startTimeout = time.Second
	// nudgeDelay is how long a resize nudge waits for output before it
	// restores the client's size.
	nudgeDelay = 200 * time.Millisecond
)

// defaultSize is the shell's size when the client doesn't send one in time.
var defaultSize = termSize{Width: 80, Height: 24}

// maxQueued is how many bytes of output may wait in a client's queue before
// the output pump stops reading the shell and waits for the client.
const maxQueued = 256 << 10

// Shell is a command running under a pty. *guestexec.PTYSession implements it.
type Shell interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(columns, rows int) error
	Wait() (int, error)
	Close() error
}

// StartFunc starts the shell of a new session with the pty size.
type StartFunc func(ctx context.Context, columns, rows int) (Shell, error)

// Registry holds the terminal sessions of one endpoint, one per key.
type Registry struct {
	mu       sync.Mutex
	sessions map[string]*session
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{sessions: map[string]*session{}}
}

// Attach handles a terminal request for the session of the key. It upgrades
// the request to a WebSocket with the v5.channel.k8s.io subprotocol, and
// answers 400 when the request doesn't offer it. Then it attaches the client
// to the key's session. When the key doesn't have a session yet, Attach
// creates one, and the session calls start to run its shell. Attach returns
// when the connection ends.
func (r *Registry) Attach(w http.ResponseWriter, req *http.Request, key string, start StartFunc) {
	log := ctrl.LoggerFrom(req.Context()).WithValues("session", key)
	offered := false
	for _, p := range websocket.Subprotocols(req) {
		offered = offered || p == remotecommand.StreamProtocolV5Name
	}
	if !offered {
		log.V(5).Info("Request doesn't offer the subprotocol", "subprotocol", remotecommand.StreamProtocolV5Name)
		http.Error(w, "Subprotocol "+remotecommand.StreamProtocolV5Name+" required", http.StatusBadRequest)
		return
	}

	c := &client{
		log:   log,
		ready: make(chan struct{}),
		wake:  make(chan struct{}, 1),
		space: make(chan struct{}, 1),
		ended: make(chan struct{}),
	}
	conn, err := Upgrade(w, req, Options{
		Subprotocols: []string{remotecommand.StreamProtocolV5Name},
		ReadLimit:    maxInput + 1,
		OnMessage:    c.onMessage,
	})
	if err != nil {
		log.V(5).Info("Failed to upgrade to WebSocket", "error", err)
		return
	}
	c.conn = conn

	// end takes the registry lock to remove a session. So under this lock,
	// the client either attaches to an ending session before end removes it,
	// and gets its status, or doesn't find it and starts a new session.
	r.mu.Lock()
	s, found := r.sessions[key]
	if !found {
		s = &session{
			registry:  r,
			key:       key,
			log:       log,
			firstSize: make(chan struct{}),
		}
		r.sessions[key] = s
	}
	c.session = s
	s.attach(c)
	r.mu.Unlock()
	close(c.ready)

	if !found {
		log.V(5).Info("Starting terminal session")
		go s.run(context.WithoutCancel(req.Context()), start)
	} else {
		log.V(5).Info("Attaching to terminal session")
	}

	c.writeLoop()
	close(c.ended)
	s.detach(c)
	conn.Close(websocket.CloseNormalClosure, "")
	log.V(5).Info("Detached from terminal session")
}

func (r *Registry) remove(s *session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions[s.key] == s {
		delete(r.sessions, s.key)
	}
}

// session is one shell and its attached client.
//
// mu guards the client, the buffer and the size state; it is never held while
// writing to a connection or to the shell. inputMu orders the shell's input,
// and resizeMu orders its size changes.
type session struct {
	registry *Registry
	key      string
	log      logr.Logger

	mu        sync.Mutex
	shell     Shell // nil until the shell runs
	client    *client
	ended     bool
	buffer    replayBuffer
	size      termSize // the client's latest size
	haveSize  bool
	firstSize chan struct{} // closed on the first size, which starts the shell
	nudge     struct {
		active  bool
		applied bool // the pty has the nudged size
		size    termSize
		gen     int
		timer   *time.Timer
	}

	inputMu sync.Mutex
	pending [][]byte // input that arrived before the shell ran
	input   Shell    // the shell, once the pending input has been written to it

	resizeMu sync.Mutex
	ptySize  termSize // the pty's current size, guarded by resizeMu
}

// attach makes c the session's client, ending the previous client's
// connection, and queues the replay. The caller holds the registry lock.
func (s *session) attach(c *client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The replay goes on c's queue under the lock that the output pump also
	// takes, so c gets every byte exactly once, either in the replay or
	// in the output after it.
	c.enqueue(frame(remotecommand.StreamStdOut, s.buffer.Replay()))
	if s.client != nil {
		s.client.finish(conflictStatus())
	}
	s.client = c
}

func (s *session) detach(c *client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == c {
		s.client = nil
	}
}

// run starts the shell once the size is known, copies its output, and ends
// the session when the shell ends.
func (s *session) run(ctx context.Context, start StartFunc) {
	timer := time.NewTimer(startTimeout)
	select {
	case <-s.firstSize:
	case <-timer.C:
	}
	timer.Stop()

	s.mu.Lock()
	size := defaultSize
	if s.haveSize {
		size = s.size
	}
	s.mu.Unlock()

	shell, err := start(ctx, size.Width, size.Height)
	if err != nil {
		s.log.V(5).Info("Failed to start shell", "error", err)
		s.end(nil, errorStatus(err))
		return
	}

	s.resizeMu.Lock()
	s.ptySize = size
	s.mu.Lock()
	s.shell = shell
	s.mu.Unlock()
	s.resizeMu.Unlock()
	// Apply a resize that arrived while the shell started.
	s.applySize()

	go s.pumpOutput(shell)

	s.inputMu.Lock()
	for _, p := range s.pending {
		if _, err := shell.Write(p); err != nil {
			break
		}
	}
	s.pending = nil
	s.input = shell
	s.inputMu.Unlock()
}

// pumpOutput copies the shell's output to the buffer and the client until
// the shell ends.
func (s *session) pumpOutput(shell Shell) {
	buf := make([]byte, 32<<10)
	for {
		n, err := shell.Read(buf)
		if n > 0 {
			s.output(buf[:n])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.V(5).Info("Shell output ended", "error", err)
			}
			break
		}
	}
	code, err := shell.Wait()
	s.log.V(5).Info("Shell ended", "status", code, "error", err)
	s.end(shell, exitStatus(code, err))
}

func (s *session) output(p []byte) {
	s.mu.Lock()
	s.buffer.Write(p)
	c := s.client
	if c != nil {
		c.enqueue(frame(remotecommand.StreamStdOut, p))
	}
	// Output from before the pty has the nudged size can't be the program's
	// redraw, so only later output ends the nudge.
	endNudge := s.nudge.active && s.nudge.applied
	if endNudge {
		s.stopNudge()
	}
	s.mu.Unlock()

	if endNudge {
		s.applySize()
	}
	if c != nil {
		c.waitForSpace()
	}
}

// end removes the session from the registry, so the next attach starts a new
// shell, then sends the status to the client and closes the shell.
func (s *session) end(shell Shell, status *metav1.Status) {
	s.registry.remove(s)
	s.mu.Lock()
	s.ended = true
	c := s.client
	s.client = nil
	s.stopNudge()
	s.mu.Unlock()
	if c != nil {
		c.finish(status)
	}
	if shell != nil {
		_ = shell.Close()
	}
}

// writeInput sends input from c to the shell, or keeps it until the shell runs.
func (s *session) writeInput(c *client, p []byte) {
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	s.mu.Lock()
	current := s.client == c
	s.mu.Unlock()
	if !current {
		return
	}
	if s.input == nil {
		s.pending = append(s.pending, p)
		return
	}
	if _, err := s.input.Write(p); err != nil {
		s.log.V(5).Info("Failed to write input", "error", err)
	}
}

// resize handles a size from c. The first size of a new session starts the
// shell. A client's first size for a running shell sets the pty one row off
// first, so that a full-screen program redraws.
func (s *session) resize(c *client, size termSize) {
	s.mu.Lock()
	if s.client != c || s.ended {
		s.mu.Unlock()
		return
	}
	first := !c.resized
	c.resized = true
	s.size = size
	if !s.haveSize {
		s.haveSize = true
		close(s.firstSize)
	}
	if s.shell != nil && first {
		nudged := size
		if size.Height > 1 {
			nudged.Height--
		} else {
			nudged.Height++
		}
		s.stopNudge()
		s.nudge.active = true
		s.nudge.size = nudged
		s.nudge.gen++
		gen := s.nudge.gen
		s.nudge.timer = time.AfterFunc(nudgeDelay, func() { s.endNudge(gen) })
	}
	s.mu.Unlock()
	s.applySize()
}

// stopNudge ends the nudge without applying the size. The caller holds mu.
func (s *session) stopNudge() {
	if s.nudge.timer != nil {
		s.nudge.timer.Stop()
		s.nudge.timer = nil
	}
	s.nudge.active = false
	s.nudge.applied = false
}

func (s *session) endNudge(gen int) {
	s.mu.Lock()
	current := s.nudge.active && s.nudge.gen == gen
	if current {
		s.stopNudge()
	}
	s.mu.Unlock()
	if current {
		s.applySize()
	}
}

// applySize sets the pty to the nudged size during a nudge, and otherwise to
// the client's latest size. Until the client sends a size, it leaves the pty
// at its starting size.
func (s *session) applySize() {
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	s.mu.Lock()
	shell := s.shell
	target := s.size
	nudging := s.nudge.active
	gen := s.nudge.gen
	if nudging {
		target = s.nudge.size
	}
	if !s.haveSize {
		target = s.ptySize
	}
	s.mu.Unlock()
	if shell == nil || target == s.ptySize {
		return
	}
	if err := shell.Resize(target.Width, target.Height); err != nil {
		s.log.V(5).Info("Failed to resize the pty", "error", err)
		return
	}
	s.ptySize = target
	if nudging {
		s.mu.Lock()
		if s.nudge.active && s.nudge.gen == gen {
			s.nudge.applied = true
		}
		s.mu.Unlock()
	}
}

// client is one WebSocket connection attached to a session. The session
// queues messages for it, and only its writeLoop writes them to the
// connection. So a slow client holds up only the shell's output, once
// maxQueued bytes wait in its queue.
type client struct {
	log     logr.Logger
	conn    *Conn
	session *session
	ready   chan struct{} // closed once the client is attached
	resized bool          // guarded by session.mu

	mu     sync.Mutex
	queue  [][]byte
	queued int
	status *metav1.Status // set when the session ends for this client

	wake  chan struct{} // signals writeLoop
	space chan struct{} // signals the output pump
	ended chan struct{} // closed when writeLoop returns
}

// onMessage handles a message from the client, on the connection's reader
// goroutine.
func (c *client) onMessage(messageType int, data []byte) {
	<-c.ready
	if messageType != websocket.BinaryMessage || len(data) == 0 {
		c.log.V(5).Info("Ignoring message", "type", messageType, "length", len(data))
		return
	}
	payload := data[1:]
	switch data[0] {
	case remotecommand.StreamStdIn:
		// ReadMessage returns a new slice for each message, so the
		// session may keep it.
		c.session.writeInput(c, payload)
	case remotecommand.StreamResize:
		size, ok := parseSize(payload)
		if !ok {
			c.log.V(5).Info("Ignoring invalid size", "size", string(payload))
			return
		}
		c.session.resize(c, size)
	default:
		c.log.V(5).Info("Ignoring message", "channel", data[0], "length", len(payload))
	}
}

func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// enqueue queues a message without waiting.
func (c *client) enqueue(msg []byte) {
	c.mu.Lock()
	if c.status == nil {
		c.queue = append(c.queue, msg)
		c.queued += len(msg)
	}
	c.mu.Unlock()
	signal(c.wake)
}

// finish queues the status after the queued output; writeLoop then closes
// the connection.
func (c *client) finish(status *metav1.Status) {
	c.mu.Lock()
	if c.status == nil {
		c.status = status
	}
	c.mu.Unlock()
	signal(c.wake)
}

// waitForSpace waits until the queue is short enough or the client is gone.
func (c *client) waitForSpace() {
	for {
		c.mu.Lock()
		ok := c.queued < maxQueued || c.status != nil
		c.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-c.space:
		case <-c.ended:
			return
		}
	}
}

// writeLoop writes the queued messages, then the status if the session ended
// for this client, and returns when it has closed the connection or the
// connection fails.
func (c *client) writeLoop() {
	for {
		c.mu.Lock()
		if len(c.queue) > 0 {
			msg := c.queue[0]
			c.queue[0] = nil
			c.queue = c.queue[1:]
			c.queued -= len(msg)
			c.mu.Unlock()
			signal(c.space)
			if err := c.conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				c.log.V(5).Info("Failed to write to WebSocket", "error", err)
				return
			}
			continue
		}
		status := c.status
		c.mu.Unlock()
		if status != nil {
			if err := c.conn.WriteMessage(websocket.BinaryMessage, statusFrame(status)); err != nil {
				c.log.V(5).Info("Failed to write status", "error", err)
			}
			c.conn.Close(websocket.CloseNormalClosure, "")
			return
		}
		select {
		case <-c.wake:
		case <-c.conn.Done():
			return
		}
	}
}
