// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package terminal implements the terminal sessions that the GUI attaches
// to over a WebSocket, with their replay buffer and channel framing. Its
// Conn also handles the WebSocket of the logs endpoint.
package terminal

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/websocket"

	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	controlTimeout = time.Second
	writeTimeout   = 10 * time.Second
	pingInterval   = 10 * time.Second
	pingTimeout    = time.Minute
)

// Options configures a connection made by [Upgrade].
type Options struct {
	// Subprotocols lists the subprotocols that the server supports, in order
	// of preference. The handshake response names the first one that the
	// client also offers. Upgrade doesn't reject a client that doesn't offer
	// any of them, so a caller that requires one checks the request first.
	Subprotocols []string
	// ReadLimit is the largest message the peer may send.
	ReadLimit int64
	// OnMessage receives each data message, on the connection's single
	// reader goroutine. Nil discards messages.
	OnMessage func(messageType int, data []byte)
}

// Conn is a server-side WebSocket connection that keeps itself alive with
// pings and fails when the peer stops answering.
//
// Any goroutine may call WriteMessage, Write and Close; writes are serialized.
type Conn struct {
	conn      *websocket.Conn
	log       logr.Logger
	writeMu   sync.Mutex
	done      chan struct{}
	doneOnce  sync.Once
	closeOnce sync.Once
}

// Upgrade upgrades the request to a WebSocket connection and starts reading
// from it. On failure, the upgrader has already answered the request.
func Upgrade(w http.ResponseWriter, req *http.Request, opts Options) (*Conn, error) {
	upgrader := websocket.Upgrader{Subprotocols: opts.Subprotocols}
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(opts.ReadLimit)
	c := &Conn{
		conn: conn,
		log:  ctrl.LoggerFrom(req.Context()),
		done: make(chan struct{}),
	}
	go c.readLoop(opts.OnMessage)
	return c, nil
}

// Done returns a channel that is closed when the connection fails, when the
// peer closes it, or when Close has closed it.
func (c *Conn) Done() <-chan struct{} {
	return c.done
}

func (c *Conn) markDone() {
	c.doneOnce.Do(func() { close(c.done) })
}

// readLoop is the only goroutine that reads from the connection, as gorilla
// requires. It also starts the goroutine that sends pings.
func (c *Conn) readLoop(onMessage func(int, []byte)) {
	timer := time.NewTimer(pingInterval)
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pingTimeout))
		timer.Reset(pingInterval)
		return nil
	})
	_ = c.conn.SetReadDeadline(time.Now().Add(pingTimeout))

	go func() {
		for {
			select {
			case <-timer.C:
				err := c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(controlTimeout))
				if err != nil {
					// The connection isn't usable anymore.
					c.markDone()
					return
				}
			case <-c.done:
				return
			}
		}
	}()

	for {
		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			// This includes [websocket.CloseError], a graceful close.
			c.markDone()
			return
		}
		// Any message shows the peer is alive, so a ping can wait.
		_ = c.conn.SetReadDeadline(time.Now().Add(pingTimeout))
		timer.Reset(pingInterval)
		if onMessage != nil {
			onMessage(messageType, data)
		}
	}
}

// WriteMessage writes one message.
func (c *Conn) WriteMessage(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.conn.WriteMessage(messageType, data)
}

// Write implements [io.Writer], writing each call as one binary message.
func (c *Conn) Write(p []byte) (int, error) {
	if err := c.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close sends a close frame with the code and text, waits up to a second for
// the peer to close its end, and then closes the connection. Only the first
// call has an effect.
func (c *Conn) Close(code int, text string) {
	c.closeOnce.Do(func() {
		message := websocket.FormatCloseMessage(code, text)
		err := c.conn.WriteControl(websocket.CloseMessage, message, time.Now().Add(controlTimeout))
		if err == nil {
			select {
			case <-time.After(controlTimeout):
			case <-c.done:
			}
		} else if !errors.Is(err, websocket.ErrCloseSent) {
			c.log.V(5).Info("Failed to close WebSocket", "error", err)
		}
		_ = c.conn.Close()
		c.markDone()
	})
}
