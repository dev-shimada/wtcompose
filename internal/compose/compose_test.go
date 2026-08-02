package compose_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dev-shimada/wtcompose/internal/compose"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestParse_OrderAndFields(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "docker-compose.yml", `
services:
  db:
    image: postgres
    container_name: myapp-db
    ports:
      - "5432:5432"
  web:
    image: nginx
    ports:
      - "8080:80"
      - "5000:5000/udp"
  worker:
    image: worker
`)

	services, err := compose.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(services) != 3 {
		t.Fatalf("got %d services, want 3", len(services))
	}

	if got, want := services[0].Name, "db"; got != want {
		t.Errorf("services[0].Name = %q, want %q (order not preserved)", got, want)
	}
	if !services[0].HasContainerName {
		t.Error("db: HasContainerName = false, want true")
	}
	if !services[0].HasPorts || len(services[0].Ports) != 1 || services[0].Ports[0] != "5432:5432" {
		t.Errorf("db: Ports = %v", services[0].Ports)
	}

	if got, want := services[1].Name, "web"; got != want {
		t.Errorf("services[1].Name = %q, want %q", got, want)
	}
	if services[1].HasContainerName {
		t.Error("web: HasContainerName = true, want false")
	}
	wantPorts := []string{"8080:80", "5000:5000/udp"}
	if len(services[1].Ports) != len(wantPorts) {
		t.Fatalf("web: Ports = %v, want %v", services[1].Ports, wantPorts)
	}
	for i, p := range wantPorts {
		if services[1].Ports[i] != p {
			t.Errorf("web: Ports[%d] = %q, want %q", i, services[1].Ports[i], p)
		}
	}

	if got, want := services[2].Name, "worker"; got != want {
		t.Errorf("services[2].Name = %q, want %q", got, want)
	}
	if services[2].HasContainerName || services[2].HasPorts {
		t.Errorf("worker: expected no container_name/ports, got %+v", services[2])
	}
}

func TestParse_NoServicesKey(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "docker-compose.yml", "version: \"3\"\n")

	if _, err := compose.Parse(path); err == nil {
		t.Fatal("Parse: expected error for missing services key, got nil")
	}
}

func TestParse_LongSyntaxPortsRejected(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "docker-compose.yml", `
services:
  web:
    ports:
      - target: 80
        published: 8080
`)

	if _, err := compose.Parse(path); err == nil {
		t.Fatal("Parse: expected error for long-syntax ports entry, got nil")
	}
}

func TestParse_MissingFile(t *testing.T) {
	if _, err := compose.Parse(filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Fatal("Parse: expected error for missing file, got nil")
	}
}
