// Package compose parses the parts of a docker-compose.yml that
// compose-worktree-override needs: for each service, whether it declares
// container_name / ports, and the ports entries themselves (short syntax
// strings and long syntax mappings alike) in document order.
package compose

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// PortSyntax distinguishes the two ways a `ports:` list entry can be
// written.
type PortSyntax int

const (
	// PortShort is the "host:container[/proto]" string form.
	PortShort PortSyntax = iota
	// PortLong is the `target:`/`published:`/... mapping form.
	PortLong
)

// Port is one entry of a service's `ports:` list.
type Port struct {
	Syntax PortSyntax

	// Raw holds the entry's scalar text, e.g. "8080:80" or
	// "8080:80/udp". Only set when Syntax is PortShort.
	Raw string

	// Node holds the entry's original mapping node, e.g.
	//   target: 80
	//   published: 8080
	//   protocol: tcp
	// Only set when Syntax is PortLong. Reassigning the published port
	// mutates this node in place (see SetPublishedPort) so re-marshaling
	// it reproduces every other field - including ones this package
	// doesn't otherwise know about - unchanged.
	Node *yaml.Node

	// Target, Published, and Protocol are extracted from Node for
	// convenience. Published and Protocol are "" when the entry omits
	// them (a bare "published" host port is picked by docker at
	// runtime; a bare protocol defaults to tcp). Only set when Syntax is
	// PortLong.
	Target    string
	Published string
	Protocol  string
}

// Service describes one service's relevant compose fields.
type Service struct {
	Name             string
	HasContainerName bool
	HasPorts         bool
	// Ports holds each ports entry, in document order. Only populated
	// when HasPorts is true.
	Ports []Port
}

// Parse reads and parses the compose file at path, returning its services
// in the order they appear in the document.
func Parse(path string) ([]Service, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("%s: empty document", path)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: expected a mapping at the document root", path)
	}

	servicesNode, ok := mappingValue(root, "services")
	if !ok {
		return nil, fmt.Errorf("%s: no top-level \"services\" key", path)
	}
	if servicesNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: \"services\" must be a mapping", path)
	}

	var services []Service
	for i := 0; i+1 < len(servicesNode.Content); i += 2 {
		nameNode := servicesNode.Content[i]
		specNode := servicesNode.Content[i+1]

		svc := Service{Name: nameNode.Value}

		if specNode.Kind != yaml.MappingNode {
			services = append(services, svc)
			continue
		}

		if _, ok := mappingValue(specNode, "container_name"); ok {
			svc.HasContainerName = true
		}

		if portsNode, ok := mappingValue(specNode, "ports"); ok {
			svc.HasPorts = true
			if portsNode.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("service %q: \"ports\" must be a list", svc.Name)
			}
			for _, item := range portsNode.Content {
				port, err := parsePort(svc.Name, item)
				if err != nil {
					return nil, err
				}
				svc.Ports = append(svc.Ports, port)
			}
		}

		services = append(services, svc)
	}

	return services, nil
}

func parsePort(serviceName string, item *yaml.Node) (Port, error) {
	switch item.Kind {
	case yaml.ScalarNode:
		return Port{Syntax: PortShort, Raw: item.Value}, nil
	case yaml.MappingNode:
		port := Port{Syntax: PortLong, Node: item}
		if v, ok := mappingValue(item, "target"); ok {
			port.Target = v.Value
		}
		if v, ok := mappingValue(item, "published"); ok {
			port.Published = v.Value
		}
		if v, ok := mappingValue(item, "protocol"); ok {
			port.Protocol = v.Value
		}
		return port, nil
	default:
		return Port{}, fmt.Errorf("service %q: unsupported ports entry", serviceName)
	}
}

// SetPublishedPort mutates a long-syntax ports entry's "published" field in
// place, so re-marshaling node reflects the reassigned host port while
// leaving every other field - and their original order - untouched. node
// must be a Port.Node from a PortLong entry that has a "published" field
// (callers only do this for entries where Port.Published was already a
// plain port number).
func SetPublishedPort(node *yaml.Node, hostPort int) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "published" {
			node.Content[i+1].Value = strconv.Itoa(hostPort)
			node.Content[i+1].Tag = "!!int"
			return
		}
	}
}

// mappingValue looks up key in a YAML mapping node, returning its value
// node and whether it was present.
func mappingValue(mapping *yaml.Node, key string) (*yaml.Node, bool) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1], true
		}
	}
	return nil, false
}
