// Package compose parses the parts of a docker-compose.yml that
// compose-worktree-override needs: for each service, whether it declares
// container_name / ports, and the raw (unparsed) ports entries in document
// order.
package compose

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Service describes one service's relevant compose fields.
type Service struct {
	Name             string
	HasContainerName bool
	HasPorts         bool
	// Ports holds the raw scalar text of each ports entry, in document
	// order. Only populated when HasPorts is true.
	Ports []string
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
				if item.Kind != yaml.ScalarNode {
					return nil, fmt.Errorf("service %q: unsupported ports entry (long syntax is not supported)", svc.Name)
				}
				svc.Ports = append(svc.Ports, item.Value)
			}
		}

		services = append(services, svc)
	}

	return services, nil
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
