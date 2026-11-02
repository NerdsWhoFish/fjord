package navigation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NerdsWhoFish/fjord/internal/lspconfig"
)

type languageRoute struct {
	language    string
	languageID  string
	displayName string
	root        string
	key         string
	profile     lspconfig.Profile
}

func routeFile(root, name string) (languageRoute, error) {
	return routeProfiles(root, name, lspconfig.Defaults())
}

var errUnconfigured = errors.New("no language server is configured for this file")

func routeProfiles(root, name string, profiles []lspconfig.Profile) (languageRoute, error) {
	if !safePath(name) {
		return languageRoute{}, errors.New("invalid source path")
	}
	p, ok := lspconfig.Match(profiles, name)
	if !ok {
		return languageRoute{}, errUnconfigured
	}
	selectedRoot, found := root, false
	for dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(name))); ; dir = filepath.Dir(dir) {
		for _, marker := range p.RootMarkers {
			if !safePath(marker) {
				return languageRoute{}, errors.New("invalid language-server root marker")
			}
			if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(marker))); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
				selectedRoot, found = dir, true
				break
			}
		}
		if found || dir == root {
			break
		}
	}
	if p.RootRequired && !found {
		return languageRoute{}, fmt.Errorf("%s navigation requires a configured root marker in this file's directory or an ancestor within the revision", p.Name)
	}
	language := p.Name
	if p.LanguageID == "opentofu-vars" {
		language = "terraform"
	}
	return languageRoute{language: language, languageID: p.LanguageID, displayName: p.Name, root: selectedRoot, key: p.Name + ":" + selectedRoot, profile: p}, nil
}
