package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dev-shimada/wtcompose/internal/registry"
)

func TestAssign_FreshAllocationsAvoidCollisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "ports.tsv")
	reg, err := registry.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	p1 := reg.Assign("repo|wt|web|8080:80")
	p2 := reg.Assign("repo|wt|web|9090:90")
	p3 := reg.Assign("repo|wt|db|5432:5432")

	if p1 == p2 || p1 == p3 || p2 == p3 {
		t.Fatalf("expected distinct ports in a single run, got %d, %d, %d", p1, p2, p3)
	}
	if p1 < registry.BasePort {
		t.Errorf("p1 = %d, want >= %d", p1, registry.BasePort)
	}
}

func TestAssign_SameKeyReturnsSamePort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ports.tsv")
	reg, err := registry.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	first := reg.Assign("repo|wt|web|8080:80")
	second := reg.Assign("repo|wt|web|8080:80")
	if first != second {
		t.Errorf("Assign called twice with same key: got %d then %d, want same value", first, second)
	}
}

func TestFlushAndReload_PersistsAcrossRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ports.tsv")

	reg1, err := registry.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	assigned := reg1.Assign("repo|wt|web|8080:80")
	if err := reg1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("registry file not written: %v", err)
	}

	reg2, err := registry.Load(path)
	if err != nil {
		t.Fatalf("Load (reload): %v", err)
	}
	reused := reg2.Assign("repo|wt|web|8080:80")
	if reused != assigned {
		t.Errorf("after reload, Assign returned %d, want persisted %d", reused, assigned)
	}
}

func TestAssign_AvoidsPortsUsedByOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ports.tsv")
	if err := os.WriteFile(path, []byte("other|repo|svc|1:1\t20000\n"), 0o644); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	reg, err := registry.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := reg.Assign("repo|wt|web|8080:80")
	if got == registry.BasePort {
		t.Errorf("Assign returned base port %d, which is already used by another key", registry.BasePort)
	}
}
