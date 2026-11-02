package navigation

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"

	"github.com/NerdsWhoFish/fjord/internal/lspconfig"
	"github.com/NerdsWhoFish/fjord/internal/schemas"
)

func prepareProfileSchemas(dir string, profile lspconfig.Profile) (lspconfig.Profile, func(), error) {
	cleanup := func() {}
	if err := lspconfig.Validate([]lspconfig.Profile{profile}); err != nil {
		return profile, cleanup, errors.New("invalid language-server profile")
	}
	data, err := json.Marshal([]any{profile.Settings, profile.InitializationOptions})
	if err != nil {
		return profile, cleanup, errors.New("invalid language-server schema settings")
	}
	if !bytes.Contains(data, []byte(schemas.Prefix)) {
		return profile, cleanup, nil
	}
	directory, err := schemas.Materialize(dir)
	if err != nil {
		return profile, cleanup, errors.New("cannot prepare bundled language-server schemas")
	}
	cleanup = func() { _ = os.RemoveAll(directory) }
	settings, err := schemas.Expand(profile.Settings, directory)
	if err != nil {
		cleanup()
		return profile, func() {}, err
	}
	options, err := schemas.Expand(profile.InitializationOptions, directory)
	if err != nil {
		cleanup()
		return profile, func() {}, err
	}
	profile.Settings, profile.InitializationOptions = settings, options
	profile.RuntimePaths = append(append([]string(nil), profile.RuntimePaths...), directory)
	return profile, cleanup, nil
}
