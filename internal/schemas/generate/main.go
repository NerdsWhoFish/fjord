// Command generate builds a resource-only Kubernetes schema from pinned definitions.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	data, err := os.ReadFile("assets/_definitions.json")
	if err != nil {
		return err
	}
	var source struct {
		Definitions map[string]struct {
			GVK []struct {
				Group   string `json:"group"`
				Version string `json:"version"`
				Kind    string `json:"kind"`
			} `json:"x-kubernetes-group-version-kind"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(data, &source); err != nil {
		return err
	}
	names := make([]string, 0, len(source.Definitions))
	for name := range source.Definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	var resources []any
	for _, name := range names {
		for _, gvk := range source.Definitions[name].GVK {
			version := gvk.Version
			if gvk.Group != "" {
				version = gvk.Group + "/" + version
			}
			resources = append(resources, map[string]any{
				"allOf": []any{
					map[string]any{"$ref": "_definitions.json#/definitions/" + name},
					map[string]any{
						"type": "object", "required": []string{"apiVersion", "kind"},
						"properties": map[string]any{
							"apiVersion": map[string]any{"enum": []string{version}},
							"kind":       map[string]any{"enum": []string{gvk.Kind}},
						},
					},
				},
			})
		}
	}
	output, err := json.MarshalIndent(map[string]any{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"title":   "Kubernetes v1.37.0 resources",
		"oneOf":   resources,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("assets/kubernetes.json", append(output, '\n'), 0644)
}
