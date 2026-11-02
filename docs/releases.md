# Releases

The **Release** GitHub Actions workflow uses [NerdsWhoFish/quill](https://github.com/NerdsWhoFish/quill) to choose a version, build before tagging, and publish one release.
Run it from `main`, choose **Release** or **Release Candidate**, and choose a patch, minor, or major bump.
With no previous version tags, a minor bump starts at `v0.1.0`.
The dry-run option is enabled by default and must be unchecked to publish.

## Published artifacts

- Fjord binaries for Linux and macOS on AMD64 and ARM64, packaged with the README and license.
- `fjord-chrome-extension.crx`, signed with the extension's permanent identity.
- `fjord-chrome-extension.zip`, for Chrome's **Load unpacked** flow.
- `checksums.txt`, covering the binary archives and both extension packages.
- Multi-platform container images at `ghcr.io/nerdswhofish/fjord`.

Quill publishes through GoReleaser and Docker in one release transaction.
The source commit is embedded in the web assets and the release version in the server binary.
Release candidates do not replace Docker's stable tags.
Publishing an image does not promote the Kubernetes deployment; its GitOps image digest is updated separately.

## Helm chart

After a successful release, the chart job packages `charts/fjord` with matching chart and application versions.
It submits the archive to [NerdsWhoFish/helm-charts](https://github.com/NerdsWhoFish/helm-charts), whose workflow validates it and publishes the Helm index to `https://charts.nerdswhofish.com`.
R2 credentials stay in that repository.

Fjord uses `HELM_APP_ID` and `HELM_APP_PRIVATE_KEY` Actions secrets to mint a short-lived token limited to the Helm repository.
The **Publish Helm chart** workflow can also publish an existing application release from `main`, or retry chart publication without releasing the application again.
Existing chart versions cannot be replaced with different contents; chart changes require a new version.
Dry-run releases do not submit charts.

## Signing identity

The repository Actions secret `FJORD_EXTENSION_SIGNING_KEY` contains an RSA private key in PEM format.
Keep this key in the approved secret manager and reuse it for every release.
Changing it changes the extension ID and breaks the installed extension's identity.
Never commit it, paste it into workflow inputs, or include it in an uploaded artifact.

The release job writes the key only to a mode-0600 file under `RUNNER_TEMP` and removes it in an unconditional cleanup step.
The packager invokes Chrome's CRX3 signer, verifies the resulting signature and public-key identity, then emits the CRX.
It includes the public key in the ZIP manifest so unpacked and signed installations share the identity.

Dry runs use a disposable signing key and a synthetic extension version `0.0.0-rc.1`.
They exercise the complete package and cross-build path without creating a tag or publishing.
Their downloadable Actions artifacts are validation builds, not production extension updates.

## Installing the extension

See the [extension guide](../extension/README.md) for platform restrictions and installation steps.
Chrome on Windows and macOS generally requires the Web Store or enterprise policy for packaged extensions.
Publishing a downloadable CRX does not bypass those restrictions.
The ZIP remains available for local development-mode installation.

## Validate locally

Use Node.js 22+, Go 1.27+, GoReleaser v2, `zip`, and Chrome.
Set `CHROME_BIN` to your Chrome executable if it is not available as `google-chrome`.
Create a disposable RSA test key outside the checkout, set `FJORD_EXTENSION_KEY_FILE` to its path, and run:

```sh
goreleaser check
actionlint
node --test extension/*.test.mjs scripts/*.test.mjs
GOWORK=off goreleaser release --snapshot --clean --skip=publish
```

Remove the disposable key after the check.
The snapshot command builds all supported native binaries and both extension artifacts without publishing.
