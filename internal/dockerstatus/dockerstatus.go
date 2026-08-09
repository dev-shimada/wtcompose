// Package dockerstatus reads `docker compose ps --format json` output and
// extracts the host ports actually published by running containers, per
// service.
package dockerstatus

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type publisher struct {
	TargetPort    int    `json:"TargetPort"`
	PublishedPort int    `json:"PublishedPort"`
	Protocol      string `json:"Protocol"`
}

type container struct {
	Service    string      `json:"Service"`
	State      string      `json:"State"`
	Publishers []publisher `json:"Publishers"`
}

// RunningPorts parses the JSON-lines output of `docker compose ps --format
// json` (one JSON object per line) and returns, for every service with at
// least one running container, its distinct published host ports in
// ascending order.
//
// Non-running containers (stopped, restarting, etc.) are ignored.
// Dual-stack publishers (docker compose reports a "0.0.0.0" and a "::"
// entry for the same host port) collapse to a single port.
func RunningPorts(output []byte) (map[string][]int, error) {
	ports := make(map[string][]int)
	seen := make(map[string]map[int]bool)

	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var c container
		if err := json.Unmarshal(line, &c); err != nil {
			return nil, fmt.Errorf("parse docker compose ps output: %w", err)
		}
		if c.State != "running" {
			continue
		}

		if seen[c.Service] == nil {
			seen[c.Service] = make(map[int]bool)
		}
		for _, p := range c.Publishers {
			if p.PublishedPort == 0 || seen[c.Service][p.PublishedPort] {
				continue
			}
			seen[c.Service][p.PublishedPort] = true
			ports[c.Service] = append(ports[c.Service], p.PublishedPort)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read docker compose ps output: %w", err)
	}

	for _, p := range ports {
		sort.Ints(p)
	}

	return ports, nil
}

// FormatToken renders ports as a compact, deterministic string suitable for
// a Herdr sidebar token, e.g. "db:5432 web:20000,20001". Services with no
// ports are omitted. Returns "" when ports is empty.
func FormatToken(ports map[string][]int) string {
	services := make([]string, 0, len(ports))
	for svc, p := range ports {
		if len(p) > 0 {
			services = append(services, svc)
		}
	}
	sort.Strings(services)

	entries := make([]string, 0, len(services))
	for _, svc := range services {
		portStrs := make([]string, len(ports[svc]))
		for i, p := range ports[svc] {
			portStrs[i] = strconv.Itoa(p)
		}
		entries = append(entries, svc+":"+strings.Join(portStrs, ","))
	}

	return strings.Join(entries, " ")
}
