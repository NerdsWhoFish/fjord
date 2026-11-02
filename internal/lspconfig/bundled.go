package lspconfig

import (
	"os"
	"path/filepath"
)

func bundledProfiles() []Profile {
	bundle := os.Getenv("FJORD_LANGUAGE_BUNDLE")
	if bundle == "" {
		bundle = "/opt/fjord/languages"
	}
	node := func(name, entry, language string, files ...string) Profile {
		runtimePaths := append([]string{bundle}, filepath.SplitList(os.Getenv("FJORD_NODE_RUNTIME_PATHS"))...)
		return Profile{
			Name: name, Command: []string{"node", "--openssl-config=/dev/null", "--max-old-space-size=512", filepath.Join(bundle, "node_modules", entry), "--stdio"},
			Files: files, LanguageID: language, Adapter: "generic", RuntimePaths: runtimePaths,
		}
	}
	yaml := node("yaml", "yaml-language-server/bin/yaml-language-server", "yaml", "**/*.yaml", "**/*.yml")
	yaml.Settings = map[string]any{
		"redhat": map[string]any{"telemetry": map[string]any{"enabled": false}},
		"yaml": map[string]any{
			"schemaStore": map[string]any{"enable": false},
			"schemas": map[string]any{
				"fjord-schema://github-workflow.json": []string{"**/.github/workflows/*.yml", "**/.github/workflows/*.yaml"},
				"fjord-schema://kubernetes.json":      []string{"**/k8s/**/*.yml", "**/k8s/**/*.yaml", "**/kubernetes/**/*.yml", "**/kubernetes/**/*.yaml"},
			},
		},
	}
	jsonProfile := func(name, language string, files ...string) Profile {
		p := node(name, "vscode-langservers-extracted/bin/vscode-json-language-server", language, files...)
		p.InitializationOptions = map[string]any{"handledSchemaProtocols": []string{"file"}}
		p.Settings = map[string]any{"json": map[string]any{
			"validate":       map[string]any{"enable": true},
			"schemaDownload": map[string]any{"enable": false},
			"schemas": []any{map[string]any{
				"fileMatch": []string{"**/tsconfig.json", "**/tsconfig.*.json", "**/jsconfig.json"},
				"url":       "fjord-schema://tsconfig.json",
			}},
		}}
		return p
	}
	script := func(name, language string, files ...string) Profile {
		p := node(name, "typescript-language-server/lib/cli.mjs", language, files...)
		p.RootMarkers = []string{"tsconfig.json", "jsconfig.json", "package.json"}
		// Never select a compiler or plugin from the untrusted PR checkout.
		p.InitializationOptions = map[string]any{
			"disableAutomaticTypingAcquisition": true,
			"tsserver":                          map[string]any{"path": filepath.Join(bundle, "node_modules/typescript/lib/tsserver.js")},
			"plugins":                           []any{}, "maxTsServerMemory": 512,
		}
		return p
	}
	python := node("python", "pyright/langserver.index.js", "python", "**/*.py", "**/*.pyi")
	python.RootMarkers = []string{"pyproject.toml", "pyrightconfig.json", "setup.py"}
	python.Settings = map[string]any{"python": map[string]any{"analysis": map[string]any{"typeCheckingMode": "basic", "diagnosticMode": "openFilesOnly", "autoSearchPaths": false}}}
	bash := node("bash", "bash-language-server/out/cli.js", "shellscript", "**/*.sh", "**/*.bash", "**/*.bashrc", "**/.bashrc", "**/.bash_profile")
	bash.Command[len(bash.Command)-1] = "start"
	bash.InitializationOptions = map[string]any{"shellcheckPath": "", "shfmt": map[string]any{"path": ""}, "explainshellEndpoint": "", "backgroundAnalysisMaxFiles": 500}
	return []Profile{
		yaml,
		jsonProfile("jsonc", "jsonc", "**/*.jsonc", "**/tsconfig.json", "**/tsconfig.*.json", "**/jsconfig.json"),
		jsonProfile("json", "json", "**/*.json"),
		script("typescript", "typescript", "**/*.ts", "**/*.mts", "**/*.cts"),
		script("typescript-react", "typescriptreact", "**/*.tsx"),
		script("javascript", "javascript", "**/*.js", "**/*.mjs", "**/*.cjs"),
		script("javascript-react", "javascriptreact", "**/*.jsx"),
		python, bash,
		node("dockerfile", "dockerfile-language-server-nodejs/bin/docker-langserver", "dockerfile", "**/Dockerfile", "**/Dockerfile.*", "**/*.Dockerfile", "**/Containerfile", "**/Containerfile.*"),
		node("html", "vscode-langservers-extracted/bin/vscode-html-language-server", "html", "**/*.html", "**/*.htm"),
		node("css", "vscode-langservers-extracted/bin/vscode-css-language-server", "css", "**/*.css"),
		node("scss", "vscode-langservers-extracted/bin/vscode-css-language-server", "scss", "**/*.scss"),
	}
}
