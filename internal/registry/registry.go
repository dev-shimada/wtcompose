// Package registry persists the host-port assignments handed out to
// worktrees, so re-running the tool for the same repo/worktree/service/port
// combination reuses the previously assigned port instead of picking a new
// one.
package registry

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// BasePort is the first host port considered for allocation.
const BasePort = 20000

// Registry is an in-memory view of the persisted port assignments, plus the
// assignments made during the current run that have not yet been flushed to
// disk.
type Registry struct {
	path    string
	entries map[string]int // key -> assigned port
	used    map[int]bool   // every port currently assigned, to any key
	pending []entry        // newly assigned entries not yet written to disk
}

type entry struct {
	key  string
	port int
}

// Load reads the registry file at path, creating an empty one if it does
// not exist yet.
func Load(path string) (*Registry, error) {
	r := &Registry{
		path:    path,
		entries: make(map[string]int),
		used:    make(map[int]bool),
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create registry dir: %w", err)
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open registry: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			continue
		}
		port, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		r.entries[fields[0]] = port
		r.used[port] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}

	return r, nil
}

// Assign returns the host port for key, reusing a previously persisted or
// already-assigned-this-run value if one exists, otherwise allocating the
// lowest free port at or above BasePort.
func (r *Registry) Assign(key string) int {
	if port, ok := r.entries[key]; ok {
		return port
	}

	port := BasePort
	for r.used[port] {
		port++
	}

	r.entries[key] = port
	r.used[port] = true
	r.pending = append(r.pending, entry{key: key, port: port})

	return port
}

// Flush appends any newly assigned entries to the registry file on disk.
func (r *Registry) Flush() error {
	if len(r.pending) == 0 {
		return nil
	}

	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open registry for write: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, e := range r.pending {
		if _, err := fmt.Fprintf(w, "%s\t%d\n", e.key, e.port); err != nil {
			return fmt.Errorf("write registry: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}

	r.pending = nil
	return nil
}
