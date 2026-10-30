import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { changesPattern, fjordURL, redirectHistory } from "./route.mjs";
import { extensionArchive } from "./package.mjs";

const manifest = JSON.parse(
  await readFile(new URL("./manifest.json", import.meta.url), "utf8"),
);
const rules = JSON.parse(
  await readFile(new URL("./rules.json", import.meta.url), "utf8"),
);
const cases = [
  [
    "https://github.com/spacelift-solutions/demo/pull/229/changes",
    "https://fjord.stout.zone/spacelift-solutions/demo/229",
  ],
  [
    "https://github.com/NerdsWhoFish/fjord/pull/7/changes/?w=1#diff-123",
    "https://fjord.stout.zone/NerdsWhoFish/fjord/7",
  ],
  [
    "https://github.com/a/.github/pull/1/changes#files",
    "https://fjord.stout.zone/a/.github/1",
  ],
  [
    "https://github.com/a/repo.name_1/pull/29/changes?tab=files",
    "https://fjord.stout.zone/a/repo.name_1/29",
  ],
  ["https://github.com/a/b/pull/1", undefined],
  ["https://github.com/a/b/pull/1/files", undefined],
  ["https://github.com/a/b/issues/1/changes", undefined],
  ["https://github.com/a/b/pull/1/changes/more", undefined],
  ["https://github.com/a/b/pull/0/changes", undefined],
  ["https://github.com/a/b/pull/1.5/changes", undefined],
  ["https://github.com/a/b/pull/01/changes", undefined],
  ["https://github.com/a/b/pull/9007199254740992/changes", undefined],
  ["https://github.com/-a/b/pull/1/changes", undefined],
  ["https://github.com/a-/b/pull/1/changes", undefined],
  ["https://github.com/a/b/pull/1/Changes", undefined],
  ["https://github.com/a/%62/pull/1/changes", undefined],
  ["https://github.com/a/../pull/1/changes", undefined],
  ["https://github.com.evil.test/a/b/pull/1/changes", undefined],
  ["https://github.com@evil.test/a/b/pull/1/changes", undefined],
  ["https://user:password@github.com/a/b/pull/1/changes", undefined], // betterleaks:allow synthetic URL rejection fixture
  ["https://sub.github.com/a/b/pull/1/changes", undefined],
  ["https://github.com:8443/a/b/pull/1/changes", undefined],
  ["http://github.com/a/b/pull/1/changes", undefined],
  ["https://fjord.stout.zone/a/b/1", undefined],
  ["not a URL", undefined],
];

function declarativeRedirect(raw, type = "main_frame", method = "get") {
  let url;
  try {
    url = new URL(raw);
  } catch {
    return undefined;
  }
  const rule = rules.find(
    ({ condition }) =>
      condition.resourceTypes.includes(type) &&
      condition.requestMethods.includes(method) &&
      new RegExp(
        condition.regexFilter,
        condition.isUrlFilterCaseSensitive ? "" : "i",
      ).test(url.href),
  );
  if (!rule) return undefined;
  const match = new RegExp(rule.condition.regexFilter).exec(url.href);
  return rule.action.redirect.regexSubstitution.replace(
    /\\([1-9])/g,
    (_, index) => match[Number(index)],
  );
}

test("static and history redirects agree for supported URLs and exclusions", () => {
  assert.equal(
    new RegExp(rules[0].condition.regexFilter).source,
    changesPattern.source,
  );
  for (const [source, target] of cases) {
    assert.equal(fjordURL(source), target, `history: ${source}`);
    assert.equal(declarativeRedirect(source), target, `declarative: ${source}`);
  }
});

test("declarative redirects ignore subframes, fetches and mutating requests", () => {
  const url = cases[0][0];
  assert.equal(declarativeRedirect(url, "sub_frame"), undefined);
  assert.equal(declarativeRedirect(url, "xmlhttprequest"), undefined);
  assert.equal(declarativeRedirect(url, "main_frame", "post"), undefined);
});

test("manifest scopes permissions and enables the runtime files", () => {
  assert.equal(manifest.manifest_version, 3);
  assert.deepEqual(
    manifest.permissions.sort(),
    ["declarativeNetRequestWithHostAccess", "webNavigation"].sort(),
  );
  assert.deepEqual(manifest.host_permissions, ["https://github.com/*"]);
  assert.equal(manifest.background.type, "module");
  assert.equal(manifest.background.service_worker, "service-worker.mjs");
  assert.deepEqual(manifest.declarative_net_request.rule_resources, [
    { id: "fjord_pull_requests", enabled: true, path: "rules.json" },
  ]);
  assert.equal(manifest.content_scripts, undefined);
});

const event = {
  tabId: 4,
  frameId: 0,
  documentLifecycle: "active",
  url: cases[0][0],
};

test("history navigation redirects only the active top-level matching page", async () => {
  const updates = [];
  const tabs = {
    get: async () => ({ url: event.url }),
    update: async (...args) => updates.push(args),
  };
  assert.equal(await redirectHistory(event, tabs), true);
  assert.deepEqual(updates, [[4, { url: cases[0][1] }]]);
  for (const ignored of [
    { ...event, frameId: 1 },
    { ...event, tabId: -1 },
    { ...event, documentLifecycle: "prerender" },
    { ...event, documentLifecycle: "cached" },
    { ...event, url: "https://github.com/a/b/issues/1" },
  ])
    assert.equal(
      await redirectHistory(ignored, {
        get: () => assert.fail("read ignored tab"),
      }),
      false,
    );
});

test("newer and pending navigations win over delayed history events", async () => {
  for (const tab of [
    { url: "https://github.com/other/repo" },
    { url: event.url, pendingUrl: "https://github.com/other/repo" },
    {},
  ])
    assert.equal(
      await redirectHistory(event, {
        get: async () => tab,
        update: () => assert.fail("replaced newer navigation"),
      }),
      false,
    );
});

test("closed tabs and failed updates do not leave rejected event promises", async () => {
  assert.equal(
    await redirectHistory(event, {
      get: async () => {
        throw new Error("closed");
      },
    }),
    false,
  );
  assert.equal(
    await redirectHistory(event, {
      get: async () => ({ url: event.url }),
      update: async () => {
        throw new Error("closed");
      },
    }),
    false,
  );
});

test("service worker registers a narrow History API event filter", async () => {
  let registered;
  globalThis.chrome = {
    webNavigation: {
      onHistoryStateUpdated: {
        addListener: (handler, filter) => {
          registered = { handler, filter };
        },
      },
    },
  };
  try {
    await import("./service-worker.mjs");
    assert.deepEqual(registered.filter, {
      url: [{ schemes: ["https"], hostEquals: "github.com" }],
    });
    assert.equal(typeof registered.handler, "function");
  } finally {
    delete globalThis.chrome;
  }
});

test("packaging is deterministic for an unchanged checkout", async () => {
  assert.deepEqual(await extensionArchive(), await extensionArchive());
});
