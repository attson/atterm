package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepo builds a throwaway repo with one commit on branch "main" and
// an uncommitted 3-line addition to a tracked file.
func initTestRepo(t *testing.T, gitPath string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitPath, append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-m", "init", "--no-gpg-sign")
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGetGitInfo(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	repo := initTestRepo(t, gitPath)
	notRepo := t.TempDir()

	app := &App{}
	got := app.GetGitInfo([]string{repo, notRepo, repo, ""})

	if len(got) != 1 {
		t.Fatalf("want exactly the repo cwd (deduped, non-repo omitted); got %+v", got)
	}
	info := got[0]
	if info.Cwd != repo {
		t.Errorf("Cwd = %q, want %q", info.Cwd, repo)
	}
	if info.Branch != "main" {
		t.Errorf("Branch = %q, want main", info.Branch)
	}
	if info.Added != 3 || info.Deleted != 0 {
		t.Errorf("Added/Deleted = %d/%d, want 3/0", info.Added, info.Deleted)
	}
}

func TestGetGitInfoDetachedHead(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	repo := initTestRepo(t, gitPath)
	cmd := exec.Command(gitPath, "-C", repo, "checkout", "--detach", "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("detach: %v\n%s", err, out)
	}

	got := (&App{}).GetGitInfo([]string{repo})
	if len(got) != 1 || got[0].Branch == "" || got[0].Branch == "main" {
		t.Fatalf("detached HEAD should report a short sha; got %+v", got)
	}
}
