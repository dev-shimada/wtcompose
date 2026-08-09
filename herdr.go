package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dev-shimada/wtcompose/internal/dockerstatus"
)

// herdrPortsToken is the Herdr metadata token name this command reports to.
// It must match the "$wtcompose_ports" row referenced in Herdr's
// [ui.sidebar.spaces] config.
const herdrPortsToken = "wtcompose_ports"

// runHerdrWatch polls `docker compose ps` in cwd and reports the running
// containers' published ports to Herdr as workspace metadata, so a
// [ui.sidebar.spaces] row referencing "$wtcompose_ports" can show, for the
// worktree's own workspace, which ports its containers are actually
// listening on right now.
//
// It runs until interrupted. There is no one-shot mode: a report with no
// refresher behind it would go stale the moment a container is
// stopped or restarted, which is worse than not reporting at all.
func runHerdrWatch(args []string) error {
	fs := flag.NewFlagSet("herdr", flag.ContinueOnError)
	interval := fs.Duration("interval", 5*time.Second, "polling interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *interval <= 0 {
		return fmt.Errorf("--interval は正の値である必要があります")
	}

	workspaceID := os.Getenv("HERDR_WORKSPACE_ID")
	if workspaceID == "" {
		return fmt.Errorf("HERDR_WORKSPACE_ID が設定されていません。herdr が管理するペインの中で実行してください")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(cwd, "docker-compose.yml")); err != nil {
		return fmt.Errorf("カレントディレクトリに docker-compose.yml がありません")
	}

	// A safety net for the case this process is killed without a chance to
	// clear its own token (kill -9, crash): the sidebar row disappears on
	// its own shortly after, instead of showing stale ports forever.
	ttlMs := (*interval * 3).Milliseconds()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("👀 %s (workspace=%s) のコンテナポートを %s ごとに herdr へ報告します。Ctrl-C で停止します。\n", cwd, workspaceID, *interval)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	var lastToken string
	for {
		token, err := pollAndReport(cwd, workspaceID, ttlMs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "⚠️  "+err.Error())
		} else if token != lastToken {
			if token == "" {
				fmt.Println("🔌 起動中のコンテナはありません")
			} else {
				fmt.Println("🔌 " + token)
			}
			lastToken = token
		}

		select {
		case <-ticker.C:
			continue
		case <-sigCh:
			if err := clearToken(workspaceID); err != nil {
				fmt.Fprintln(os.Stderr, "⚠️  "+err.Error())
			}
			return nil
		}
	}
}

// pollAndReport runs `docker compose ps` once and reports (or clears) the
// ports token accordingly, returning the token that's now in effect.
func pollAndReport(cwd, workspaceID string, ttlMs int64) (string, error) {
	cmd := exec.Command("docker", "compose", "ps", "--format", "json")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		if clearErr := clearToken(workspaceID); clearErr != nil {
			fmt.Fprintln(os.Stderr, "⚠️  "+clearErr.Error())
		}
		if ee, ok := err.(*exec.ExitError); ok {
			if stderr := strings.TrimSpace(string(ee.Stderr)); stderr != "" {
				return "", fmt.Errorf("docker compose ps: %s", stderr)
			}
		}
		return "", fmt.Errorf("docker compose ps: %w", err)
	}

	ports, err := dockerstatus.RunningPorts(out)
	if err != nil {
		return "", err
	}
	token := dockerstatus.FormatToken(ports)

	if token == "" {
		return "", clearToken(workspaceID)
	}

	reportCmd := exec.Command("herdr", "workspace", "report-metadata", workspaceID,
		"--source", "wtcompose",
		"--token", herdrPortsToken+"="+token,
		"--ttl-ms", strconv.FormatInt(ttlMs, 10),
	)
	if out, err := reportCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("herdr workspace report-metadata: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return token, nil
}

func clearToken(workspaceID string) error {
	cmd := exec.Command("herdr", "workspace", "report-metadata", workspaceID,
		"--source", "wtcompose",
		"--clear-token", herdrPortsToken,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("herdr workspace report-metadata --clear-token: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
