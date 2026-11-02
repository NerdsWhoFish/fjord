import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createPrivateKey, generateKeyPairSync } from "node:crypto";
import { mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import { test } from "node:test";

const run = promisify(execFile);
const script = new URL("./prepare-extension-key.mjs", import.meta.url).pathname;
async function fixture(t) {
  const directory = await mkdtemp(join(tmpdir(), "fjord-key-test-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  return {
    directory,
    path: join(directory, "fjord-extension-signing.pem"),
    env: { RUNNER_TEMP: directory, GITHUB_ENV: join(directory, "github-env") },
  };
}
function key() {
  return generateKeyPairSync("rsa", {
    modulusLength: 2048,
    privateKeyEncoding: { type: "pkcs8", format: "pem" },
    publicKeyEncoding: { type: "spki", format: "pem" },
  }).privateKey;
}

test("dry runs create an ephemeral private key and always clean it up", async (t) => {
  const f = await fixture(t);
  const result = await run(process.execPath, [script], {
    env: { ...f.env, RELEASE_DRY_RUN: "true", FJORD_EXTENSION_SIGNING_KEY: "ignored" },
  });
  assert.equal(result.stdout, "");
  assert.equal(result.stderr, "");
  assert.equal(createPrivateKey(await readFile(f.path)).asymmetricKeyType, "rsa");
  assert.equal((await stat(f.path)).mode & 0o777, 0o600);
  assert.equal(await readFile(f.env.GITHUB_ENV, "utf8"), `FJORD_EXTENSION_KEY_FILE=${f.path}\n`);
  await run(process.execPath, [script, "--cleanup"], { env: f.env });
  await assert.rejects(stat(f.path), { code: "ENOENT" });
});

test("publishing requires and preserves the persistent signing identity", async (t) => {
  const f = await fixture(t);
  await assert.rejects(run(process.execPath, [script], { env: f.env }), /Configure FJORD_EXTENSION_SIGNING_KEY/);
  const pem = key();
  const result = await run(process.execPath, [script], {
    env: { ...f.env, RELEASE_DRY_RUN: "false", FJORD_EXTENSION_SIGNING_KEY: pem },
  });
  assert.equal(await readFile(f.path, "utf8"), pem);
  assert.equal(result.stdout + result.stderr, "");
});

test("invalid secrets fail without echoing their value or creating a file", async (t) => {
  const f = await fixture(t);
  const invalid = "private-key-placeholder-that-must-not-appear-in-logs";
  await assert.rejects(
    run(process.execPath, [script], { env: { ...f.env, FJORD_EXTENSION_SIGNING_KEY: invalid } }),
    (error) => {
      assert.match(error.stderr, /must be a valid RSA PEM key/);
      assert.ok(!error.stderr.includes(invalid));
      return true;
    },
  );
  await assert.rejects(stat(f.path), { code: "ENOENT" });
});
