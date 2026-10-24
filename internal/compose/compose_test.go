package compose_test

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"

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
	if !services[0].HasPorts || len(services[0].Ports) != 1 ||
		services[0].Ports[0].Syntax != compose.PortShort || services[0].Ports[0].Raw != "5432:5432" {
		t.Errorf("db: Ports = %+v", services[0].Ports)
	}

	if got, want := services[1].Name, "web"; got != want {
		t.Errorf("services[1].Name = %q, want %q", got, want)
	}
	if services[1].HasContainerName {
		t.Error("web: HasContainerName = true, want false")
	}
	wantPorts := []string{"8080:80", "5000:5000/udp"}
	if len(services[1].Ports) != len(wantPorts) {
		t.Fatalf("web: Ports = %+v, want %v", services[1].Ports, wantPorts)
	}
	for i, p := range wantPorts {
		if services[1].Ports[i].Syntax != compose.PortShort || services[1].Ports[i].Raw != p {
			t.Errorf("web: Ports[%d] = %+v, want Raw %q", i, services[1].Ports[i], p)
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

func TestParse_LongSyntax(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "docker-compose.yml", `
services:
  web:
    ports:
      - target: 80
        published: 8080
        protocol: tcp
      - target: 90
        published: "8000-8010"
      - target: 100
`)

	services, err := compose.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(services) != 1 || len(services[0].Ports) != 3 {
		t.Fatalf("got %+v, want 1 service with 3 ports", services)
	}
	ports := services[0].Ports

	full := ports[0]
	if full.Syntax != compose.PortLong || full.Target != "80" || full.Published != "8080" || full.Protocol != "tcp" {
		t.Errorf("ports[0] = %+v, want target=80 published=8080 protocol=tcp", full)
	}
	if full.Node == nil {
		t.Error("ports[0].Node is nil, want the original mapping node")
	}

	rangePort := ports[1]
	if rangePort.Syntax != compose.PortLong || rangePort.Published != "8000-8010" {
		t.Errorf("ports[1] = %+v, want published=8000-8010 (a range, preserved verbatim)", rangePort)
	}

	noPublished := ports[2]
	if noPublished.Syntax != compose.PortLong || noPublished.Target != "100" || noPublished.Published != "" {
		t.Errorf("ports[2] = %+v, want target=100 published=\"\" (omitted)", noPublished)
	}
}

func TestParse_InvalidPortsEntry(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "docker-compose.yml", `
services:
  web:
    ports:
      - - 80
        - 8080
`)

	if _, err := compose.Parse(path); err == nil {
		t.Fatal("Parse: expected error for a ports entry that's neither a scalar nor a mapping, got nil")
	}
}

func TestParse_MissingFile(t *testing.T) {
	if _, err := compose.Parse(filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Fatal("Parse: expected error for missing file, got nil")
	}
}

func TestSetPublishedPort(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "docker-compose.yml", `
services:
  web:
    ports:
      - target: 80
        published: 8080
        protocol: tcp
`)

	services, err := compose.Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	node := services[0].Ports[0].Node

	compose.SetPublishedPort(node, 20000)

	out, err := yaml.Marshal(node)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	want := "target: 80\npublished: 20000\nprotocol: tcp\n"
	if string(out) != want {
		t.Errorf("after SetPublishedPort, marshaled node =\n%s\nwant:\n%s", out, want)
	}
}
