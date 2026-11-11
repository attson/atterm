package termius

import "testing"

func TestTermiusProcessNames(t *testing.T) {
	for _, name := range []string{"termius", "termius-app", "Termius.exe", `C:\Users\test\Termius.exe`, "/Applications/Termius.app/Contents/MacOS/Termius", "Termius Helper (Renderer)", "Termius Helper"} {
		if !isTermiusProcess(name) {
			t.Errorf("missed running process %q", name)
		}
	}
	for _, name := range []string{"", "atermius", "termius-exporter", "termius-data-exporter.exe", "sh", "ps", "atterm", "Termius.app"} {
		if isTermiusProcess(name) {
			t.Errorf("false positive for %q", name)
		}
	}
}
