# Fjord PR Review for Chrome

Redirect GitHub's pull request changes page to your Fjord review service.

```text
https://github.com/spacelift-solutions/demo/pull/229/changes
https://fjord.stout.zone/spacelift-solutions/demo/229
```

The extension handles a newly opened changes page and GitHub's in-page navigation to that page.
It accepts a trailing slash, query parameters and a fragment, but removes GitHub-specific parameters and fragments from the Fjord destination.
Other GitHub pages, other hosts, embedded frames and non-GET requests stay put.
The destination is fixed to `https://fjord.stout.zone`.

## Download and install

GitHub releases include a [signed CRX3 package](https://github.com/NerdsWhoFish/fjord/releases/latest/download/fjord-chrome-extension.crx) and a [ZIP bundle](https://github.com/NerdsWhoFish/fjord/releases/latest/download/fjord-chrome-extension.zip).
The CRX uses a persistent signing key, so releases retain the same extension ID.
The release ZIP includes that public key to retain the same identity when loaded unpacked.

A downloadable CRX is not a Chrome Web Store listing.
Chrome restricts off-store installations on Windows and macOS; downloading this file does not bypass those restrictions.
Use the ZIP with **Load unpacked** for local testing, or an approved managed-browser distribution policy where supported.
Linux supports self-hosted CRX installation.
See Chrome's official [installation methods and platform restrictions](https://developer.chrome.com/docs/extensions/how-to/distribute/install-extensions) and [Linux self-hosting guide](https://developer.chrome.com/docs/extensions/how-to/distribute/host-on-linux).

### Load unpacked

Chrome 106 or newer is required.

1. Open `chrome://extensions`.
2. Enable **Developer mode**.
3. Choose **Load unpacked**, then select this repository's `extension` directory.
4. Visit a GitHub pull request URL ending in `/changes`.

For a downloaded ZIP, unzip it and choose its extracted folder in step 3.
Disable the extension in `chrome://extensions` whenever you want GitHub's original changes view.

Fjord must already be reachable at `https://fjord.stout.zone`.
Installing this extension does not deploy the service or configure DNS, TLS or authentication.
If that address is unavailable, disable the extension to return to GitHub.

## Permissions and implementation

There are no content scripts, dependencies, build step, storage, page-content access, credential handling or analytics.

- `declarativeNetRequestWithHostAccess` and the sole host permission, `https://github.com/*`, let Chrome redirect matching main-frame GET requests before loading the GitHub page.
- `webNavigation` observes URL history changes so GitHub's SPA navigation also redirects. The listener filters HTTPS GitHub events and ignores subframes, inactive documents, closed tabs and events superseded by newer navigation.
- No `tabs` permission is requested. The GitHub host permission permits reading the matching tab's current URL; navigating a tab does not need that permission.

The routing rules accept GitHub owner and repository characters and positive PR numbers of up to 15 digits.
No query strings, fragments or source-page contents are forwarded to Fjord.

Official Chrome references:

- [Declarative Net Request](https://developer.chrome.com/docs/extensions/reference/api/declarativeNetRequest)
- [History navigation events](https://developer.chrome.com/docs/extensions/reference/api/webNavigation#event-onHistoryStateUpdated)
- [Tabs permissions](https://developer.chrome.com/docs/extensions/reference/api/tabs#permissions)

When changing the routing expression, also verify Chrome's `declarativeNetRequest.isRegexSupported({regex, requireCapturing: true})` returns `isSupported: true`.
Chrome can ignore a rule that exceeds its compiled RE2 memory budget even when the same expression passes JavaScript tests.
Exercise an initial navigation and a `history.pushState()` transition in a disposable browser profile before distributing a changed extension.

## Check and package

From the repository root:

```sh
node --test extension/*.test.mjs
node extension/package.mjs artifacts/fjord-chrome-extension.zip
```

Packaging requires Node.js 20 or newer and the standard `zip` command, with no npm dependencies.
It writes a deterministic ZIP containing only the extension runtime files, README and the repository's MIT license.
Repeating it from the same checkout produces identical bytes.

### Signed release packaging

Chrome's built-in packer produces the CRX3 signature.
Packaging requires an existing unencrypted RSA PEM private key with at least 2048 bits; it never creates a new release signing identity.
Keep that key in the approved secret manager and configure the repository's `FJORD_EXTENSION_SIGNING_KEY` Actions secret with the PEM contents.
Changing or losing the key changes the extension ID and breaks the existing installation's update identity.
The release workflow writes the secret to a temporary file with mode `0600` and removes it after packaging.

```sh
export CHROME_BIN=/path/to/google-chrome
export FJORD_EXTENSION_KEY_FILE=/secure/path/to/fjord-extension.pem
node extension/package.mjs artifacts/fjord-chrome-extension.zip \
  --version v1.2.3 \
  --crx artifacts/fjord-chrome-extension.crx
```

`--key-file` can supply the key-file path instead of `FJORD_EXTENSION_KEY_FILE`.
Never pass the private-key contents as a command argument.
`CHROME_BIN` defaults to `google-chrome`; on macOS it can point to `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`.
The packager validates the emitted CRX3 signature and extension ID against the configured key before writing release artifacts.
The key, packaging code, and tests are excluded from both bundles.

Chrome versions contain four integers, each at most 65535.
Stable `v1.2.3` is packaged as `1.2.3.65535`, and `v1.2.3-rc.4` becomes `1.2.3.4`.
This keeps a stable release newer than its candidates.
The manifest's `version_name` displays the original release version.
Candidates must be `rc.1` through `rc.65534`; unsupported versions fail before packaging.

Set `CHROME_BIN` when running the tests to exercise the real packer, verify signatures and tamper rejection, and confirm that two versions share one extension ID:

```sh
CHROME_BIN=google-chrome node --test extension/*.test.mjs
```

This extension is covered by the repository's [MIT license](../LICENSE).
