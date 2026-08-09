// Command wtcompose generates a docker-compose.override.yml that gives the
// current git worktree its own container_name and host ports, so several
// worktrees of the same repo can run `docker compose up` at the same time
// without colliding.
//
// It is a Go port of the compose-worktree-override shell function: same
// registry file, same override marker, same behavior, no dependency on yq.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dev-shimada/wtcompose/internal/compose"
	"github.com/dev-shimada/wtcompose/internal/gitinfo"
	"github.com/dev-shimada/wtcompose/internal/override"
	"github.com/dev-shimada/wtcompose/internal/registry"
)

const overrideFilename = "docker-compose.override.yml"

// hostPortRegex matches `ports:` entries with an explicit host mapping,
// e.g. "8080:80" or "8080:80/udp". Anything else (bind-address prefixed
// entries, port ranges, container-only ports, env-var interpolations) is
// passed through unchanged, since its host port can't be safely reassigned
// here.
var hostPortRegex = regexp.MustCompile(`^[0-9]+:([0-9]+)(/(tcp|udp))?$`)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "herdr" {
		if err := runHerdrWatch(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "❌ "+err.Error())
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "❌ "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	composePath := filepath.Join(cwd, "docker-compose.yml")
	if _, err := os.Stat(composePath); err != nil {
		return fmt.Errorf("カレントディレクトリに docker-compose.yml がありません")
	}

	services, err := compose.Parse(composePath)
	if err != nil {
		return fmt.Errorf("docker-compose.yml の解析に失敗しました: %w", err)
	}

	overridePath := filepath.Join(cwd, overrideFilename)
	if existing, err := os.ReadFile(overridePath); err == nil {
		if !strings.Contains(string(existing), override.Marker) {
			return fmt.Errorf("既存の %s が見つかりました(このツールが生成したものではありません)。上書きしません", overrideFilename)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	info, err := gitinfo.Resolve(cwd)
	if err != nil {
		return err
	}

	namePrefix := info.RepoName
	if info.RepoName != info.WorktreeName {
		namePrefix = info.RepoName + "-" + info.WorktreeName
	}

	// Matches the original shell function's hardcoded path exactly
	// (it does not honor XDG_CONFIG_HOME), so an existing registry
	// built by the shell version is reused as-is.
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	regPath := filepath.Join(home, ".config", "compose-worktree-override", "ports.tsv")
	reg, err := registry.Load(regPath)
	if err != nil {
		return err
	}

	overrideServices := make([]override.Service, 0, len(services))
	for _, svc := range services {
		ov := override.Service{Name: svc.Name, HasPorts: svc.HasPorts}
		if svc.HasContainerName {
			ov.ContainerName = namePrefix + "-" + svc.Name
		}
		if svc.HasPorts {
			ov.Ports = make([]string, 0, len(svc.Ports))
			for _, raw := range svc.Ports {
				ov.Ports = append(ov.Ports, resolvePort(reg, info.RepoName, info.WorktreeName, svc.Name, raw))
			}
		}
		overrideServices = append(overrideServices, ov)
	}

	content := override.Build(overrideServices)
	if content == "" {
		if err := os.Remove(overridePath); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Println("ℹ️  container_name / ports を持つserviceがないため、上書きは不要です")
		return nil
	}

	// Persist port assignments before writing the override file: if this
	// were reversed and Flush failed, the override on disk could claim
	// ports the registry doesn't know are taken, letting another worktree
	// hand out the same ports later.
	if err := reg.Flush(); err != nil {
		return err
	}
	if err := os.WriteFile(overridePath, []byte(content), 0o644); err != nil {
		return err
	}
	if info.CommonDir != "" {
		if err := gitinfo.RegisterExclude(info.CommonDir, overrideFilename); err != nil {
			return err
		}
	}

	fmt.Printf("✅ %s を生成しました(%s/%s)\n", overrideFilename, info.RepoName, info.WorktreeName)
	fmt.Print(content)
	return nil
}

// resolvePort reassigns the host side of a "host:container[/proto]" ports
// entry to a worktree-unique port, persisted in the registry. Entries that
// don't match that exact shape are returned unchanged.
func resolvePort(reg *registry.Registry, repo, worktree, service, raw string) string {
	m := hostPortRegex.FindStringSubmatch(raw)
	if m == nil {
		return raw
	}
	containerPort, proto := m[1], m[3]

	key := strings.Join([]string{repo, worktree, service, raw}, "|")
	hostPort := reg.Assign(key)

	if proto != "" {
		return fmt.Sprintf("%d:%s/%s", hostPort, containerPort, proto)
	}
	return fmt.Sprintf("%d:%s", hostPort, containerPort)
}
