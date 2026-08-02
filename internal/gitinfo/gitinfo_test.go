package gitinfo_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dev-shimada/wtcompose/internal/gitinfo"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestResolve_MainWorktree(t *testing.T) {
	base := t.TempDir()
	repoDir := filepath.Join(base, "myrepo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "init", "-q")
	runGit(t, repoDir, "commit", "-q", "--allow-empty", "-m", "init")

	info, err := gitinfo.Resolve(repoDir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if info.RepoName != "myrepo" {
		t.Errorf("RepoName = %q, want %q", info.RepoName, "myrepo")
	}
	if info.WorktreeName != "myrepo" {
		t.Errorf("WorktreeName = %q, want %q", info.WorktreeName, "myrepo")
	}
	if info.CommonDir == "" {
		t.Error("CommonDir is empty, want the resolved .git dir")
	}
}

func TestResolve_LinkedWorktree(t *testing.T) {
	base := t.TempDir()
	repoDir := filepath.Join(base, "myrepo")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "init", "-q")
	runGit(t, repoDir, "commit", "-q", "--allow-empty", "-m", "init")

	wtDir := filepath.Join(base, "myrepo-feature")
	runGit(t, repoDir, "worktree", "add", "-q", "-b", "feature", wtDir)

	info, err := gitinfo.Resolve(wtDir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if info.RepoName != "myrepo" {
		t.Errorf("RepoName = %q, want %q (should resolve to main worktree name, not the linked worktree)", info.RepoName, "myrepo")
	}
	if info.WorktreeName != "myrepo-feature" {
		t.Errorf("WorktreeName = %q, want %q", info.WorktreeName, "myrepo-feature")
	}

	mainGitDir, err := filepath.EvalSymlinks(filepath.Join(repoDir, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	resolvedCommon, err := filepath.EvalSymlinks(info.CommonDir)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedCommon != mainGitDir {
		t.Errorf("CommonDir = %q, want it to resolve to the main repo's .git (%q)", info.CommonDir, mainGitDir)
	}
}

func TestResolve_NotAGitRepo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plainproject")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	info, err := gitinfo.Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if info.CommonDir != "" {
		t.Errorf("CommonDir = %q, want empty outside a git repo", info.CommonDir)
	}
	if info.RepoName != "plainproject" || info.WorktreeName != "plainproject" {
		t.Errorf("RepoName/WorktreeName = %q/%q, want both %q", info.RepoName, info.WorktreeName, "plainproject")
	}
}

func TestRegisterExclude_AddsOnceAndIsIdempotent(t *testing.T) {
	commonDir := filepath.Join(t.TempDir(), ".git")

	if err := gitinfo.RegisterExclude(commonDir, "docker-compose.override.yml"); err != nil {
		t.Fatalf("RegisterExclude: %v", err)
	}
	if err := gitinfo.RegisterExclude(commonDir, "docker-compose.override.yml"); err != nil {
		t.Fatalf("RegisterExclude (second call): %v", err)
	}

	data, err := os.ReadFile(filepath.Join(commonDir, "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude file: %v", err)
	}
	got := string(data)
	want := "docker-compose.override.yml\n"
	if got != want {
		t.Errorf("exclude file = %q, want %q (entry should appear exactly once)", got, want)
	}
}
