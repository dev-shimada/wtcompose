package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// herdrWatcherPIDFile returns the path used to track a background
// `wtcompose herdr` watcher started on behalf of a given herdr workspace, so
// `wtcompose up` doesn't start a second one on top of an already-running
// watcher, and `wtcompose down` knows what to stop. Keyed by workspace ID
// (not directory) since that's what the watcher actually reports to, and
// it's stable across how the directory was reached.
func herdrWatcherPIDFile(workspaceID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "compose-worktree-override", "herdr-watchers", workspaceID+".pid"), nil
}

// maybeStartHerdrWatcher starts `wtcompose herdr` in the background for cwd
// when running inside a herdr pane and no watcher is already running for
// this workspace. Best-effort: failures are reported but never block the
// docker compose command this is a side effect of.
func maybeStartHerdrWatcher(cwd string) {
	workspaceID := os.Getenv("HERDR_WORKSPACE_ID")
	if workspaceID == "" {
		return // not running inside a herdr pane, nothing to do
	}

	pidFile, err := herdrWatcherPIDFile(workspaceID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "⚠️  herdr ウォッチャーの起動確認に失敗しました: "+err.Error())
		return
	}
	if pid, alive := readAlivePID(pidFile); alive {
		_ = pid
		return // already watching this workspace
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "⚠️  herdr ウォッチャーの起動に失敗しました: "+err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "⚠️  herdr ウォッチャーの起動に失敗しました: "+err.Error())
		return
	}
	logFile, err := os.OpenFile(strings.TrimSuffix(pidFile, ".pid")+".log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "⚠️  herdr ウォッチャーの起動に失敗しました: "+err.Error())
		return
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "herdr")
	cmd.Dir = cwd
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach from this session so it outlives it

	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "⚠️  herdr ウォッチャーの起動に失敗しました: "+err.Error())
		return
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "⚠️  herdr ウォッチャーの pid 記録に失敗しました: "+err.Error())
	}
	fmt.Fprintf(os.Stderr, "👀 herdr ウォッチャーをバックグラウンドで起動しました(pid=%d, log=%s)\n", cmd.Process.Pid, logFile.Name())
}

// maybeStopHerdrWatcher stops the background watcher started for this
// workspace by maybeStartHerdrWatcher, if any. Best-effort, same as start.
func maybeStopHerdrWatcher() {
	workspaceID := os.Getenv("HERDR_WORKSPACE_ID")
	if workspaceID == "" {
		return
	}

	pidFile, err := herdrWatcherPIDFile(workspaceID)
	if err != nil {
		return
	}
	pid, alive := readAlivePID(pidFile)
	if !alive {
		os.Remove(pidFile)
		return
	}

	// SIGTERM triggers the watcher's own shutdown path (clears its herdr
	// token, then exits) rather than just killing it outright.
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		fmt.Fprintln(os.Stderr, "⚠️  herdr ウォッチャーの停止に失敗しました: "+err.Error())
	}
	os.Remove(pidFile)
}

// readAlivePID reads a pid file and confirms the process it names is still
// alive (signal 0 is a pure existence/permission check, no-op on the
// process itself). Stale or missing files report not-alive without error.
func readAlivePID(pidFile string) (int, bool) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return 0, false
	}
	return pid, true
}
