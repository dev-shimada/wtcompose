package dockerstatus_test

import (
	"testing"

	"github.com/dev-shimada/wtcompose/internal/dockerstatus"
)

func TestRunningPorts(t *testing.T) {
	// Modeled on real `docker compose ps --format json` output: one JSON
	// object per line, dual-stack Publishers entries for the same port,
	// and a mix of running/non-running containers.
	output := []byte(`
{"Service":"web","State":"running","Publishers":[{"URL":"0.0.0.0","TargetPort":80,"PublishedPort":20000,"Protocol":"tcp"},{"URL":"::","TargetPort":80,"PublishedPort":20000,"Protocol":"tcp"},{"URL":"0.0.0.0","TargetPort":443,"PublishedPort":20001,"Protocol":"tcp"}]}
{"Service":"db","State":"running","Publishers":[{"URL":"0.0.0.0","TargetPort":5432,"PublishedPort":20002,"Protocol":"tcp"}]}
{"Service":"worker","State":"exited","Publishers":[]}
`)

	ports, err := dockerstatus.RunningPorts(output)
	if err != nil {
		t.Fatalf("RunningPorts: %v", err)
	}

	if got, want := ports["web"], []int{20000, 20001}; !equal(got, want) {
		t.Errorf("web ports = %v, want %v (dual-stack entries should dedup)", got, want)
	}
	if got, want := ports["db"], []int{20002}; !equal(got, want) {
		t.Errorf("db ports = %v, want %v", got, want)
	}
	if _, ok := ports["worker"]; ok {
		t.Errorf("worker: exited container should not be included, got %v", ports["worker"])
	}
}

func TestRunningPorts_NoContainers(t *testing.T) {
	ports, err := dockerstatus.RunningPorts([]byte(""))
	if err != nil {
		t.Fatalf("RunningPorts: %v", err)
	}
	if len(ports) != 0 {
		t.Errorf("ports = %v, want empty", ports)
	}
}

func TestRunningPorts_ContainerWithNoPublishedPorts(t *testing.T) {
	output := []byte(`{"Service":"worker","State":"running","Publishers":[]}` + "\n")

	ports, err := dockerstatus.RunningPorts(output)
	if err != nil {
		t.Fatalf("RunningPorts: %v", err)
	}
	if _, ok := ports["worker"]; ok {
		t.Errorf("worker has no published ports, should not appear in map, got %v", ports["worker"])
	}
}

func TestRunningPorts_InvalidJSON(t *testing.T) {
	if _, err := dockerstatus.RunningPorts([]byte("not json")); err == nil {
		t.Fatal("RunningPorts: expected error for invalid JSON, got nil")
	}
}

func TestFormatToken(t *testing.T) {
	cases := []struct {
		name  string
		ports map[string][]int
		want  string
	}{
		{
			name:  "empty",
			ports: map[string][]int{},
			want:  "",
		},
		{
			name:  "single service single port",
			ports: map[string][]int{"web": {20000}},
			want:  "web:20000",
		},
		{
			name:  "multiple services sorted by name",
			ports: map[string][]int{"web": {20000, 20001}, "db": {20002}},
			want:  "db:20002 web:20000,20001",
		},
		{
			name:  "service with no ports omitted",
			ports: map[string][]int{"web": {20000}, "worker": {}},
			want:  "web:20000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dockerstatus.FormatToken(tc.ports); got != tc.want {
				t.Errorf("FormatToken(%v) = %q, want %q", tc.ports, got, tc.want)
			}
		})
	}
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
