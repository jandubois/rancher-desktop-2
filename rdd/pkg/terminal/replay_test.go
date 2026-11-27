// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package terminal

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func replayOf(writes ...string) string {
	var b replayBuffer
	for _, w := range writes {
		b.Write([]byte(w))
	}
	return string(b.Replay())
}

// assertReplay compares replays, and on a mismatch reports their lengths and
// first bytes instead of up to a megabyte of output.
func assertReplay(t *testing.T, want, got string) {
	t.Helper()
	head := func(s string) string { return s[:min(len(s), 40)] }
	assert.Assert(t, got == want, "got %d bytes starting %q, want %d bytes starting %q",
		len(got), head(got), len(want), head(want))
}

func TestReplayStart(t *testing.T) {
	fill := strings.Repeat("x", replayLimit)
	tests := []struct {
		name   string
		writes []string
		want   string
	}{
		{"empty", nil, ""},
		{"under the limit", []string{"one\n", "two\n"}, "one\ntwo\n"},
		{"after the first line feed", []string{fill, "a\nb\nc"}, "b\nc"},
		{"at a UTF-8 lead byte", []string{strings.Repeat("é", replayLimit/2), "z"}, strings.Repeat("é", replayLimit/2-1) + "z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertReplay(t, tt.want, replayOf(tt.writes...))
		})
	}
}

func modesOf(output ...string) modes {
	var tr modeTracker
	for _, o := range output {
		tr.parse([]byte(o))
	}
	return tr.modes
}

func TestModeTracking(t *testing.T) {
	tests := []struct {
		name   string
		output []string
		want   modes
	}{
		{"alternate screen 1049", []string{"\x1b[?1049h"}, modes{altScreen: true}},
		{"alternate screen 47", []string{"\x1b[?47h"}, modes{altScreen: true}},
		{"alternate screen 1047 reset", []string{"\x1b[?1047h", "\x1b[?1047l"}, modes{}},
		{"cursor keys", []string{"\x1b[?1h"}, modes{appCursorKeys: true}},
		{"cursor keys reset", []string{"\x1b[?1h\x1b[?1l"}, modes{}},
		{"mouse, last one wins", []string{"\x1b[?1000h\x1b[?1003h"}, modes{mouse: 1003}},
		{"mouse, reset of the active mode", []string{"\x1b[?1002h\x1b[?1002l"}, modes{}},
		{"mouse, reset of another mode", []string{"\x1b[?9h\x1b[?1000l"}, modes{mouse: 9}},
		{"SGR mouse", []string{"\x1b[?1006h"}, modes{sgrMouse: true}},
		{"bracketed paste", []string{"\x1b[?2004h"}, modes{bracketedPaste: true}},
		{"bracketed paste reset", []string{"\x1b[?2004h\x1b[?2004l"}, modes{}},
		{"hidden cursor", []string{"\x1b[?25l"}, modes{cursorHidden: true}},
		{"visible cursor", []string{"\x1b[?25l\x1b[?25h"}, modes{}},
		{"several parameters", []string{"\x1b[?1049;1;2004h"}, modes{altScreen: true, appCursorKeys: true, bracketedPaste: true}},
		{"split across writes", []string{"\x1b[", "?10", "49", "h"}, modes{altScreen: true}},
		{"RIS", []string{"\x1b[?1049h\x1b[?25l\x1bc"}, modes{}},
		{"OSC ended by BEL", []string{"\x1b]0;[?1049h\x07", "[?1h"}, modes{}},
		{"OSC ended by ST", []string{"\x1b]0;?1049h\x1b\\", "\x1b[?1h"}, modes{appCursorKeys: true}},
		{"other CSI final", []string{"\x1b[1049h\x1b[?1049m"}, modes{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, modesOf(tt.output...))
		})
	}
}

func TestReplayModePrefix(t *testing.T) {
	fill := strings.Repeat("x", replayLimit)
	t.Run("inside a full-screen program", func(t *testing.T) {
		got := replayOf("\x1b[?1049h\x1b[?1h\x1b[?1002h\x1b[?1006h\x1b[?2004h\x1b[?25l", fill, "\nscreen")
		assertReplay(t, "\x1b[?1049h\x1b[?1h\x1b[?1002h\x1b[?1006h\x1b[?2004h\x1b[?25lscreen", got)
	})
	t.Run("after the program exits", func(t *testing.T) {
		got := replayOf("\x1b[?1049h", fill, "\x1b[?1049l\nprompt")
		assertReplay(t, "prompt", got)
	})
	t.Run("sequence split by the drop boundary", func(t *testing.T) {
		// The buffer drops the first four bytes, "\x1b[?1"; the replay
		// skips the rest of the sequence up to the line feed.
		got := replayOf("\x1b[?1049h\n", strings.Repeat("y", replayLimit-5))
		assertReplay(t, "\x1b[?1049h"+strings.Repeat("y", replayLimit-5), got)
	})
	t.Run("sequence completed after it dropped out", func(t *testing.T) {
		var b replayBuffer
		b.Write([]byte("\x1b[?10"))
		b.Write([]byte("49h"))
		b.Write([]byte(fill + "\nz"))
		assertReplay(t, "\x1b[?1049hz", string(b.Replay()))
	})
}
