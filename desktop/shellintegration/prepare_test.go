package shellintegration

import "testing"

func TestPrepareReturnsZeroPlanWhenDisabled(t *testing.T) {
	got := Prepare("/bin/zsh", false, "sid")
	if got.Shell != "" || len(got.ExtraEnv) != 0 || len(got.ExtraArgs) != 0 || got.Cleanup != nil {
		t.Fatalf("Prepare disabled returned non-zero plan: %+v", got)
	}
}

func TestPrepareReturnsZeroPlanForUnknownShell(t *testing.T) {
	got := Prepare("/bin/cmd.exe", true, "sid")
	if got.Shell != "" || len(got.ExtraEnv) != 0 || len(got.ExtraArgs) != 0 || got.Cleanup != nil {
		t.Fatalf("Prepare unknown shell returned non-zero plan: %+v", got)
	}
}

func TestPrepareReturnsZeroPlanForEmptyPath(t *testing.T) {
	got := Prepare("", true, "sid")
	if got.Shell != "" || len(got.ExtraEnv) != 0 || len(got.ExtraArgs) != 0 || got.Cleanup != nil {
		t.Fatalf("Prepare empty path returned non-zero plan: %+v", got)
	}
}
