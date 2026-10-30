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

## Install locally

Chrome 106 or newer is required.

1. Open `chrome://extensions`.
2. Enable **Developer mode**.
3. Choose **Load unpacked**, then select this repository's `extension` directory.
4. Visit a GitHub pull request URL ending in `/changes`.

For a packaged build, unzip the archive and choose its extracted folder in step 3.
The archive is an unpacked extension bundle, not a signed `.crx` or a Chrome Web Store release.
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
node --test extension/extension.test.mjs
node extension/package.mjs artifacts/fjord-chrome-extension.zip
```

Packaging requires Node.js 20 or newer and the standard `zip` command, with no npm dependencies.
It writes a deterministic ZIP containing only the extension runtime files, README and the repository's MIT license.
Repeating it from the same checkout produces identical bytes.

This extension is covered by the repository's [MIT license](../LICENSE).
