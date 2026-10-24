// Package gitinfo resolves the repo/worktree identity used to namespace
// generated container names and registry keys, and registers the override
// file in the shared (non-committed) git exclude list.
package gitinfo

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Info holds the identity of the current directory with respect to git.
type Info struct {
	// RepoName is the basename of the main worktree's root directory, or
	// the basename of the current directory when not inside a git repo.
	RepoName string
	// WorktreeName is the basename of the current (physical, symlink
	// resolved) directory.
	WorktreeName string
	// CommonDir is the absolute path to the shared ".git" directory
	// (`git rev-parse --git-common-dir`), or "" when not inside a git
	// repo.
	CommonDir string
}

// Resolve determines repo/worktree identity for the current directory cwd.
// Being outside a git repository is not an error: CommonDir is left empty
// and RepoName falls back to the directory name, matching the original
// shell implementation.
func Resolve(cwd string) (Info, error) {
	worktreeBase := cwd
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		worktreeBase = resolved
	}
	worktreeName := filepath.Base(worktreeBase)

	commonDir, err := gitCommonDir(cwd)
	if err != nil || commonDir == "" {
		return Info{
			RepoName:     worktreeName,
			WorktreeName: worktreeName,
			CommonDir:    "",
		}, nil
	}

	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(cwd, commonDir)
	}
	commonDir = filepath.Clean(commonDir)

	repoName := filepath.Base(filepath.Dir(commonDir))

	return Info{
		RepoName:     repoName,
		WorktreeName: worktreeName,
		CommonDir:    commonDir,
	}, nil
}

func gitCommonDir(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", nil // not a git repo (or git missing) - not an error for us
	}
	return strings.TrimSpace(string(out)), nil
}

// RegisterExclude appends filename to commonDir/info/exclude, unless it is
// already listed there. info/exclude is shared across all worktrees of a
// repo but is never committed, which is why it's used to hide the
// generated, worktree-local override file from `git status`.
func RegisterExclude(commonDir, filename string) (err error) {
	excludeDir := filepath.Join(commonDir, "info")
	if err := os.MkdirAll(excludeDir, 0o755); err != nil {
		return err
	}
	excludePath := filepath.Join(excludeDir, "exclude")

	if already, err := containsExactLine(excludePath, filename); err != nil {
		return err
	} else if already {
		return nil
	}

	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	_, err = f.WriteString(filename + "\n")
	return err
}

func containsExactLine(path, want string) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if scanner.Text() == want {
			return true, nil
		}
	}
	return false, scanner.Err()
}
