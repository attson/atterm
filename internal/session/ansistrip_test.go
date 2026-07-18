package session

import "testing"

func TestStripANSI(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "hello world\n", "hello world\n"},
		{"csi color", "\x1b[31mred\x1b[0m", "red"},
		{"csi cursor move", "\x1b[2Jclear", "clear"},
		{"osc bel", "\x1b]0;title\x07hello", "hello"},
		{"osc st", "\x1b]0;title\x1b\\hello", "hello"},
		{"esc single char", "\x1b=raw", "raw"},
		{"mixed inline", "[\x1b[1mbold\x1b[0m] ok\n", "[bold] ok\n"},
		{"preserves crlf and tabs", "a\r\nb\tc\n", "a\r\nb\tc\n"},
		{"truncated csi at eof", "abc\x1b[3", "abc"},
		{"truncated osc at eof", "abc\x1b]title", "abc"},
		{"empty", "", ""},

		// Leading orphan CSI tail — tail buffer cut mid-sequence. Without
		// the stripLeadingCSITail guard, these would emit garbage like
		// "153;153;153m..." in Feishu cards.
		{"orphan SGR truecolor at start", "153;153;153mEnter to select", "Enter to select"},
		{"orphan SGR basic at start", "31mred text", "red text"},
		{"orphan CSI cursor at start", "2Jcleared", "cleared"},
		{"orphan multi-param at start", "1;5;7mfancy", "fancy"},

		// Non-orphan inputs that LOOK orphan-ish must be preserved.
		{"leading digits then text — not orphan", "123 results found", "123 results found"},
		{"leading digit;digit;text — not orphan (no final byte)", "12;34 columns", "12;34 columns"},
		{"semicolon-only — no run, not orphan", ";abc", ";abc"},
		{"single digit then space — not CSI", "1 thing", "1 thing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(StripANSI([]byte(tc.in)))
			if got != tc.want {
				t.Fatalf("StripANSI(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
