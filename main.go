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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"go.yaml.in/yaml/v3"

	"github.com/dev-shimada/wtcompose/internal/compose"
	"github.com/dev-shimada/wtcompose/internal/gitinfo"
	"github.com/dev-shimada/wtcompose/internal/override"
	"github.com/dev-shimada/wtcompose/internal/registry"
)

const overrideFilename = "docker-compose.override.yml"

// hostPortRegex matches short-syntax `ports:` entries with an explicit host
// mapping, e.g. "8080:80" or "8080:80/udp". Anything else (bind-address
// prefixed entries, port ranges, container-only ports, env-var
// interpolations) is passed through unchanged, since its host port can't be
// safely reassigned here.
var hostPortRegex = regexp.MustCompile(`^[0-9]+:([0-9]+)(/(tcp|udp))?$`)

// singlePortRegex matches a long-syntax entry's "published" field when it's
// a single plain port number. Anything else (absent, or a range like
// "8000-8010") is left as-is, for the same reason as hostPortRegex above.
var singlePortRegex = regexp.MustCompile(`^[0-9]+$`)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "herdr":
			if err := runHerdrWatch(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "❌ "+err.Error())
				os.Exit(1)
			}
			return
		case "completion":
			if err := runCompletionScript(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "❌ "+err.Error())
				os.Exit(1)
			}
			return
		case "__complete":
			if err := runComplete(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "❌ "+err.Error())
				os.Exit(1)
			}
			return
		default:
			if err := runComposePassthrough(os.Args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, "❌ "+err.Error())
				os.Exit(1)
			}
			return
		}
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "❌ "+err.Error())
		os.Exit(1)
	}
}

// runComposePassthrough regenerates the override file (so any docker
// compose invocation below picks up this worktree's unique container_name
// / ports) and then execs `docker compose <args>` in place of this
// process, so stdio, the tty, signals, and the exit code are all exactly
// docker compose's own.
//
// Commands that don't operate on a specific project (--help/-h anywhere,
// or "version"/"ls"/"help" as the subcommand) skip both the override
// regeneration and its docker-compose.yml requirement, so they keep
// working outside any compose project directory, exactly like `docker
// compose --help` does.
func runComposePassthrough(args []string) error {
	targetsProject := needsComposeFile(args)

	if targetsProject {
		if reason, explicit := explicitComposeFile(args); explicit {
			return fmt.Errorf("%s により明示的な compose ファイルが指定されているため、docker-compose.override.yml は自動読み込みされず wtcompose のポート分離は働きません。この操作は `docker compose ...` を直接実行してください", reason)
		}

		if err := generateOverride(os.Stderr); err != nil {
			return err
		}
	}

	// Inside a herdr pane, keep the sidebar ports display alive without the
	// user having to remember to start/stop `wtcompose herdr` themselves:
	// spawn it in the background on `up`, stop it on `down`. Skipped for
	// --help/version/etc (targetsProject false), since those aren't
	// actually starting or stopping anything.
	if targetsProject && len(args) > 0 {
		switch args[0] {
		case "up":
			if cwd, err := os.Getwd(); err == nil {
				maybeStartHerdrWatcher(cwd)
			}
		case "down":
			maybeStopHerdrWatcher()
		}
	}

	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker が見つかりません: %w", err)
	}

	argv := append([]string{"docker", "compose"}, args...)
	if err := syscall.Exec(dockerPath, argv, os.Environ()); err != nil {
		return fmt.Errorf("docker compose の実行に失敗しました: %w", err)
	}
	return nil // unreachable on success: syscall.Exec replaces this process
}

// needsComposeFile reports whether args target a specific compose project
// (and therefore need docker-compose.yml / the generated override) as
// opposed to a discovery/meta command like --help, version, or ls that
// docker compose itself answers without one.
func needsComposeFile(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return false
		}
	}
	if len(args) > 0 {
		switch args[0] {
		case "version", "ls", "help":
			return false
		}
	}
	return true
}

// explicitComposeFile reports whether args or the environment select an
// explicit compose file list, which makes docker compose skip its default
// file discovery (and therefore skip docker-compose.override.yml) even
// though wtcompose just wrote one.
func explicitComposeFile(args []string) (reason string, explicit bool) {
	for _, a := range args {
		if a == "-f" || a == "--file" || strings.HasPrefix(a, "--file=") {
			return "-f/--file", true
		}
	}
	if os.Getenv("COMPOSE_FILE") != "" {
		return "COMPOSE_FILE 環境変数", true
	}
	return "", false
}

func run() error {
	return generateOverride(os.Stdout)
}

// generateOverride parses docker-compose.yml in the current directory and
// (re)writes docker-compose.override.yml with this worktree's unique
// container_name / ports, reporting progress to out. Bare `wtcompose`
// writes to stdout; compose-passthrough invocations write to stderr so
// stdout stays byte-identical to the wrapped docker compose command.
func generateOverride(out io.Writer) error {
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
			ov.Ports = make([]override.PortLine, 0, len(svc.Ports))
			for _, p := range svc.Ports {
				switch p.Syntax {
				case compose.PortShort:
					ov.Ports = append(ov.Ports, override.PortLine{
						Content: resolveShortPort(reg, info.RepoName, info.WorktreeName, svc.Name, p.Raw),
					})
				case compose.PortLong:
					content, err := resolveLongPort(reg, info.RepoName, info.WorktreeName, svc.Name, p)
					if err != nil {
						return err
					}
					ov.Ports = append(ov.Ports, override.PortLine{Content: content, Long: true})
				}
			}
		}
		overrideServices = append(overrideServices, ov)
	}

	content := override.Build(overrideServices)
	if content == "" {
		if err := os.Remove(overridePath); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Fprintln(out, "ℹ️  container_name / ports を持つserviceがないため、上書きは不要です")
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

	fmt.Fprintf(out, "✅ %s を生成しました(%s/%s)\n", overrideFilename, info.RepoName, info.WorktreeName)
	fmt.Fprint(out, content)
	return nil
}

// resolveShortPort reassigns the host side of a "host:container[/proto]"
// ports entry to a worktree-unique port, persisted in the registry. Entries
// that don't match that exact shape are returned unchanged.
func resolveShortPort(reg *registry.Registry, repo, worktree, service, raw string) string {
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

// resolveLongPort reassigns a long-syntax ports entry's "published" field
// to a worktree-unique port, the same way resolveShortPort does for the
// short-syntax form, then renders the whole entry (every other field
// preserved as originally written) as YAML content ready for
// override.PortLine. Entries whose "published" isn't a single plain port
// number (absent, or a range) are rendered unchanged.
func resolveLongPort(reg *registry.Registry, repo, worktree, service string, p compose.Port) (string, error) {
	if singlePortRegex.MatchString(p.Published) {
		protocol := p.Protocol
		if protocol == "" {
			protocol = "tcp" // compose's own default, so key identity doesn't shift if it's later spelled out explicitly
		}
		key := strings.Join([]string{repo, worktree, service, "long", p.Target, p.Published, protocol}, "|")
		hostPort := reg.Assign(key)
		compose.SetPublishedPort(p.Node, hostPort)
	}

	data, err := yaml.Marshal(p.Node)
	if err != nil {
		return "", fmt.Errorf("service %q: ports エントリのレンダリングに失敗しました: %w", service, err)
	}
	return string(data), nil
}
