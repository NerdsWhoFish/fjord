// Package schemas supplies pinned offline schemas to isolated language servers.
package schemas

import (
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const Prefix = "fjord-schema://"

//go:generate go run ./generate

//go:embed assets/*
var assets embed.FS

// Materialize creates a private bundle under a trusted runtime parent. The
// caller owns its lifetime and must expose it read-only to the language server.
func Materialize(parent string) (directory string, err error) {
	if !filepath.IsAbs(parent) || filepath.Clean(parent) != parent {
		return "", errors.New("schema parent must be a clean absolute path")
	}
	directory, err = os.MkdirTemp(parent, "schemas-")
	if err != nil {
		return "", fmt.Errorf("create schema bundle: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(directory)
		}
	}()
	entries, err := assets.ReadDir("assets")
	if err != nil {
		return directory, err
	}
	for _, entry := range entries {
		data, readErr := assets.ReadFile("assets/" + entry.Name())
		if readErr != nil {
			return directory, readErr
		}
		if err = os.WriteFile(filepath.Join(directory, entry.Name()), data, 0400); err != nil {
			return directory, fmt.Errorf("write schema bundle: %w", err)
		}
	}
	return directory, nil
}

// Expand copies JSON-shaped configuration and resolves bundle placeholders in
// both strings and keys, because YAML's schema map uses schema URIs as keys.
func Expand(configuration map[string]any, directory string) (map[string]any, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, errors.New("schema directory must be a clean absolute path")
	}
	if configuration == nil {
		return nil, nil
	}
	value, err := expand(configuration, directory)
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

func expand(value any, directory string) (any, error) {
	switch value := value.(type) {
	case string:
		if !strings.HasPrefix(value, Prefix) {
			return value, nil
		}
		name := strings.TrimPrefix(value, Prefix)
		switch name {
		case "kubernetes.json", "github-workflow.json", "tsconfig.json":
		default:
			return nil, errors.New("unknown bundled schema")
		}
		return (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(directory, name))}).String(), nil
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, child := range value {
			resolvedKey, err := expand(key, directory)
			if err != nil {
				return nil, err
			}
			resolved, err := expand(child, directory)
			if err != nil {
				return nil, err
			}
			if _, exists := result[resolvedKey.(string)]; exists {
				return nil, errors.New("duplicate resolved schema setting")
			}
			result[resolvedKey.(string)] = resolved
		}
		return result, nil
	case []any:
		result := make([]any, len(value))
		for i, child := range value {
			resolved, err := expand(child, directory)
			if err != nil {
				return nil, err
			}
			result[i] = resolved
		}
		return result, nil
	case []string:
		result := make([]string, len(value))
		for i, child := range value {
			resolved, err := expand(child, directory)
			if err != nil {
				return nil, err
			}
			result[i] = resolved.(string)
		}
		return result, nil
	default:
		return value, nil
	}
}
