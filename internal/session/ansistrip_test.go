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

func TestANSIStreamStripperPreservesStateAcrossChunks(t *testing.T) {
	raw := []byte("before\x1b]133;A\x07\x1b[31mred\x1b[0mafter")
	for split := 1; split < len(raw); split++ {
		var stripper ANSIStreamStripper
		got := append(stripper.Strip(raw[:split]), stripper.Strip(raw[split:])...)
		if string(got) != "beforeredafter" {
			t.Fatalf("split=%d: got %q, want %q", split, got, "beforeredafter")
		}
	}
}

func TestANSIStreamStripperDropsC1ControlStrings(t *testing.T) {
	var stripper ANSIStreamStripper
	raw := []byte{'a', 0x9d, '1', '3', '3', ';', 'A', 0x9c, 'b'}
	if got := string(stripper.Strip(raw)); got != "ab" {
		t.Fatalf("Strip(C1 OSC) = %q, want %q", got, "ab")
	}
}

func TestANSIStreamStripperPreservesC1ValuesInsideUTF8(t *testing.T) {
	// U+0750..U+075F encode with continuation bytes in the raw C1 range,
	// including the CSI/OSC/ST byte values handled by the terminal parser.
	want := "边界\u0750\u0758\u075b\u075c\u075d\u075e\u075f"
	var stripper ANSIStreamStripper
	var got []byte
	for _, b := range []byte(want) {
		got = append(got, stripper.Strip([]byte{b})...)
	}
	if string(got) != want {
		t.Fatalf("byte-wise UTF-8 stream = %q, want %q", got, want)
	}
}
