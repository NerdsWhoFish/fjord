#!/bin/sh
set -eu

export GOWORK=off
go install golang.org/x/tools/gopls@v0.24.0

# tofu-ls requires its module's replace directive, which go install pkg@version rejects.
go mod download github.com/opentofu/tofu-ls@v0.5.3
go -C "$(go env GOMODCACHE)/github.com/opentofu/tofu-ls@v0.5.3" install .
