package session

import (
	"reflect"
	"testing"
)

func TestScanOSCTitles_OSC0_BEL(t *testing.T) {
	data := []byte("\x1b]0;hello\x07rest")
	titles, consumed, ok := scanOSCTitles(data)
	// No incomplete OSC remains at the tail → caller may drop everything,
	// not just up to the terminator.
	if !ok || consumed != len(data) || !reflect.DeepEqual(titles, []string{"hello"}) {
		t.Fatalf("got titles=%v consumed=%d ok=%v", titles, consumed, ok)
	}
}

func TestScanOSCTitles_OSC1_BEL(t *testing.T) {
	titles, _, ok := scanOSCTitles([]byte("\x1b]1;tab-name\x07"))
	if !ok || !reflect.DeepEqual(titles, []string{"tab-name"}) {
		t.Fatalf("got titles=%v ok=%v", titles, ok)
	}
}

func TestScanOSCTitles_OSC2_ST(t *testing.T) {
	titles, _, ok := scanOSCTitles([]byte("\x1b]2;window-name\x1b\\"))
	if !ok || !reflect.DeepEqual(titles, []string{"window-name"}) {
		t.Fatalf("got titles=%v ok=%v", titles, ok)
	}
}

func TestScanOSCTitles_MultipleLastWins(t *testing.T) {
	data := []byte("\x1b]2;first\x07middle\x1b]2;second\x07")
	titles, consumed, ok := scanOSCTitles(data)
	if !ok || !reflect.DeepEqual(titles, []string{"first", "second"}) || consumed != len(data) {
		t.Fatalf("got titles=%v consumed=%d ok=%v", titles, consumed, ok)
	}
}

func TestScanOSCTitles_OverlongDropped(t *testing.T) {
	overlong := make([]byte, 257)
	for i := range overlong {
		overlong[i] = 'x'
	}
	data := append([]byte("\x1b]2;"), overlong...)
	data = append(data, '\x07')
	data = append(data, []byte("\x1b]2;short\x07")...)
	titles, _, ok := scanOSCTitles(data)
	if !ok || !reflect.DeepEqual(titles, []string{"short"}) {
		t.Fatalf("got titles=%v ok=%v", titles, ok)
	}
}

func TestScanOSCTitles_IncompleteLeavesTail(t *testing.T) {
	data := []byte("done\x07\x1b]2;unfinished")
	titles, consumed, ok := scanOSCTitles(data)
	if ok {
		t.Fatalf("expected ok=false, got titles=%v consumed=%d", titles, consumed)
	}
	want := len("done\x07")
	if consumed != want {
		t.Fatalf("consumed=%d want=%d", consumed, want)
	}
}

func TestScanOSCTitles_NoOSC(t *testing.T) {
	titles, consumed, ok := scanOSCTitles([]byte("plain text\x07"))
	if ok || titles != nil || consumed != len("plain text\x07") {
		t.Fatalf("got titles=%v consumed=%d ok=%v", titles, consumed, ok)
	}
}

func TestScanOSCTitles_StrippedControlChars(t *testing.T) {
	titles, _, ok := scanOSCTitles([]byte("\x1b]2;hello\rworld\x07"))
	if !ok || !reflect.DeepEqual(titles, []string{"helloworld"}) {
		t.Fatalf("got titles=%v ok=%v", titles, ok)
	}
}

func TestScanOSCTitles_InvalidUTF8Dropped(t *testing.T) {
	titles, _, ok := scanOSCTitles([]byte("\x1b]2;\xff\xfe\xfd\x07\x1b]2;ok\x07"))
	if !ok || !reflect.DeepEqual(titles, []string{"ok"}) {
		t.Fatalf("got titles=%v ok=%v", titles, ok)
	}
}
