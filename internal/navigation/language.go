package navigation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type languageRoute struct {
	language    string
	languageID  string
	displayName string
	root        string
	key         string
}

func routeFile(root, name string) (languageRoute, error) {
	if !safePath(name) {
		return languageRoute{}, errors.New("invalid source path")
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".go"):
		for dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(name))); ; dir = filepath.Dir(dir) {
			if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && info.Mode().IsRegular() {
				return languageRoute{language: "go", languageID: "go", displayName: "Go", root: dir, key: "go:" + dir}, nil
			}
			if dir == root {
				break
			}
		}
		return languageRoute{}, errors.New("Go navigation requires a go.mod in this file's directory or a parent directory within the revision")
	case strings.HasSuffix(lower, ".tf"), strings.HasSuffix(lower, ".tofu"), strings.HasSuffix(lower, ".tf.json"):
		return languageRoute{language: "terraform", languageID: "opentofu", displayName: "OpenTofu/Terraform", root: root, key: "terraform"}, nil
	case strings.HasSuffix(lower, ".tfvars"), strings.HasSuffix(lower, ".tfvars.json"):
		return languageRoute{language: "terraform", languageID: "opentofu-vars", displayName: "OpenTofu/Terraform", root: root, key: "terraform"}, nil
	case strings.HasSuffix(lower, ".py"), strings.HasSuffix(lower, ".pyi"):
		return languageRoute{}, errors.New("Python language navigation is not configured; Go and OpenTofu/Terraform module files are supported")
	case strings.HasSuffix(lower, ".tofu.json"), strings.HasSuffix(lower, ".tofuvars"), strings.HasSuffix(lower, ".tofuvars.json"):
		return languageRoute{}, errors.New("this tofu-ls integration supports .tf, .tf.json, .tfvars, .tfvars.json and .tofu files; this OpenTofu format is not supported")
	case strings.HasSuffix(lower, ".hcl"):
		return languageRoute{}, errors.New("navigation for generic HCL, lock files, and Terraform test files is not supported; select a .tf, .tofu, or variable file")
	default:
		return languageRoute{}, errors.New("language navigation is not supported for this file; select a Go or OpenTofu/Terraform module file")
	}
}
