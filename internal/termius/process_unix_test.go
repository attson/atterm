//go:build !windows

package termius

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequireStopped(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"tray process", "#!/bin/sh\nprintf 'systemd\\ntermius-app\\n'\n", "termius_running"},
		{"helper remains", "#!/bin/sh\nprintf 'Termius Helper (Renderer)\\n'\n", "termius_running"},
		{"stopped", "#!/bin/sh\nprintf 'atterm\\ntermius-exporter\\n'\n", ""},
		{"cannot inspect", "#!/bin/sh\nexit 1\n", "termius_process_check_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(tc.script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			err := RequireStopped()
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
}
