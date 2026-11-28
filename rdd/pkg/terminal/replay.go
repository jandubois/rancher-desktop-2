// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package terminal

import (
	"bytes"
	"strconv"
)

// replayLimit is how much output a session keeps for a client that attaches.
const replayLimit = 1 << 20

// replayBuffer keeps the last replayLimit bytes of output, and the terminal
// modes in force where they start.
type replayBuffer struct {
	data    []byte
	start   int // data[start:] is the content
	dropped bool
	modes   modeTracker // the state after parsing every dropped byte
}

// Write appends output, dropping the oldest bytes beyond replayLimit.
func (b *replayBuffer) Write(p []byte) {
	b.data = append(b.data, p...)
	if excess := len(b.data) - b.start - replayLimit; excess > 0 {
		b.modes.parse(b.data[b.start : b.start+excess])
		b.start += excess
		b.dropped = true
	}
	// Move the content to the front once more than replayLimit bytes have
	// been dropped, so the slice stays within twice the limit plus one write.
	if b.start > replayLimit {
		n := copy(b.data, b.data[b.start:])
		b.data = b.data[:n]
		b.start = 0
	}
}

// Replay returns the bytes that restore the terminal for a client. They set
// the modes in force where the replay starts, then repeat the buffered
// output. When the buffer has dropped bytes, the output starts after its
// first line feed. Without a line feed, it starts at the first byte that
// begins a UTF-8 character.
func (b *replayBuffer) Replay() []byte {
	content := b.data[b.start:]
	skip := 0
	if b.dropped {
		if i := bytes.IndexByte(content, '\n'); i >= 0 {
			skip = i + 1
		} else {
			for skip < len(content) && content[skip]&0xC0 == 0x80 {
				skip++
			}
		}
	}
	modes := b.modes
	modes.parse(content[:skip])
	prefix := modes.prefix()
	replay := make([]byte, 0, len(prefix)+len(content)-skip)
	replay = append(replay, prefix...)
	return append(replay, content[skip:]...)
}

// Mouse tracking modes; at most one is active.
const (
	mouseX10       = 9
	mouseNormal    = 1000
	mouseButton    = 1002
	mouseAnyMotion = 1003
)

// modes are the terminal modes that a replay restores. The zero value is the
// terminal's default state.
type modes struct {
	altScreen      bool
	appCursorKeys  bool
	mouse          int // 0 or the active mouse tracking mode
	sgrMouse       bool
	bracketedPaste bool
	cursorHidden   bool
}

func (m *modes) set(mode int, on bool) {
	switch mode {
	case 47, 1047, 1049:
		m.altScreen = on
	case 1:
		m.appCursorKeys = on
	case mouseX10, mouseNormal, mouseButton, mouseAnyMotion:
		if on {
			m.mouse = mode
		} else if m.mouse == mode {
			m.mouse = 0
		}
	case 1006:
		m.sgrMouse = on
	case 2004:
		m.bracketedPaste = on
	case 25:
		m.cursorHidden = !on
	}
}

// prefix returns a DECSET or DECRST sequence for each mode that differs
// from its default.
func (m *modes) prefix() []byte {
	var buf []byte
	decset := func(mode int, on bool) {
		buf = append(buf, "\x1b[?"...)
		buf = strconv.AppendInt(buf, int64(mode), 10)
		if on {
			buf = append(buf, 'h')
		} else {
			buf = append(buf, 'l')
		}
	}
	if m.altScreen {
		decset(1049, true)
	}
	if m.appCursorKeys {
		decset(1, true)
	}
	if m.mouse != 0 {
		decset(m.mouse, true)
	}
	if m.sgrMouse {
		decset(1006, true)
	}
	if m.bracketedPaste {
		decset(2004, true)
	}
	if m.cursorHidden {
		decset(25, false)
	}
	return buf
}

type parserState int

const (
	stateGround             parserState = iota
	stateEscape                         // after ESC
	stateEscapeIntermediate             // after ESC and an intermediate byte, such as ESC (
	stateCSI                            // inside a control sequence
	stateString                         // inside an OSC, DCS, SOS, PM or APC string
	stateStringEscape                   // after ESC inside a string
)

// maxParams is the number of control sequence parameters that the parser keeps.
const maxParams = 16

// modeTracker parses terminal output one byte at a time and applies DECSET
// and DECRST sequences to its modes. It keeps its parser state between calls,
// so a sequence may be split anywhere. It doesn't hold pointers or slices,
// so Replay can parse with a copy and leave the buffer's tracker unchanged.
type modeTracker struct {
	modes   modes
	state   parserState
	private bool // the sequence started with "?"
	invalid bool // the sequence can't be a DECSET or DECRST
	params  [maxParams]int
	nParams int
}

func (t *modeTracker) prefix() []byte {
	return t.modes.prefix()
}

func (t *modeTracker) parse(p []byte) {
	for _, c := range p {
		t.parseByte(c)
	}
}

func (t *modeTracker) parseByte(c byte) {
	// CAN and SUB cancel a sequence; ESC starts a new one.
	switch c {
	case 0x18, 0x1a:
		t.state = stateGround
		return
	case 0x1b:
		if t.state == stateString {
			t.state = stateStringEscape
		} else {
			t.state = stateEscape
		}
		return
	}

	switch t.state {
	case stateGround:
	case stateEscape:
		switch {
		case c == '[':
			t.state = stateCSI
			t.private = false
			t.invalid = false
			t.params = [maxParams]int{}
			t.nParams = 0
		case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
			t.state = stateString
		case c == 'c':
			t.modes = modes{}
			t.state = stateGround
		case c >= 0x20 && c <= 0x2f:
			t.state = stateEscapeIntermediate
		default:
			t.state = stateGround
		}
	case stateEscapeIntermediate:
		if c >= 0x30 && c <= 0x7e {
			t.state = stateGround
		}
	case stateCSI:
		t.parseCSI(c)
	case stateString:
		if c == 0x07 {
			t.state = stateGround
		}
	case stateStringEscape:
		// ESC \ ends the string. Any other byte after the ESC ends the string
		// too, and the ESC starts a new sequence.
		if c == '\\' {
			t.state = stateGround
		} else {
			t.state = stateEscape
			t.parseByte(c)
		}
	}
}

func (t *modeTracker) parseCSI(c byte) {
	switch {
	case c >= '0' && c <= '9':
		if t.nParams == 0 {
			t.nParams = 1
		}
		i := t.nParams - 1
		if i < maxParams && t.params[i] < 100000 {
			t.params[i] = t.params[i]*10 + int(c-'0')
		}
	case c == ';':
		if t.nParams == 0 {
			t.nParams = 1
		}
		t.nParams++
	case c == '?' && t.nParams == 0 && !t.private:
		t.private = true
	case c >= 0x3a && c <= 0x3f:
		// A colon, or a private marker in the wrong place.
		t.invalid = true
	case c >= 0x20 && c <= 0x2f:
		// An intermediate byte makes it a different sequence.
		t.invalid = true
	case c >= 0x40 && c <= 0x7e:
		t.state = stateGround
		if t.private && !t.invalid && (c == 'h' || c == 'l') {
			for i := range min(t.nParams, maxParams) {
				t.modes.set(t.params[i], c == 'h')
			}
		}
	default:
		// A control character inside a sequence takes effect on its own
		// and doesn't end the sequence.
	}
}
