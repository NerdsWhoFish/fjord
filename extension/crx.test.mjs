import test from "node:test";
import assert from "node:assert/strict";
import { generateKeyPairSync } from "node:crypto";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import { extensionPackage } from "./package.mjs";
import { extensionVersion, signingPublicKey, verifyCrx3 } from "./crx.mjs";

const run = promisify(execFile);

test("release versions fit Chrome and stable upgrades follow candidates", () => {
  assert.deepEqual(extensionVersion("v1.2.3"), {
    version: "1.2.3.65535",
    version_name: "1.2.3",
  });
  assert.deepEqual(extensionVersion("1.2.3-rc.4"), {
    version: "1.2.3.4",
    version_name: "1.2.3-rc.4",
  });
  assert.equal(extensionVersion("0.0.0-rc.1").version, "0.0.0.1");
  assert.equal(
    extensionVersion("65535.65535.65535").version,
    "65535.65535.65535.65535",
  );
  const versions = ["1.2.3-rc.1", "1.2.3-rc.65534", "1.2.3", "1.2.4-rc.1"].map(
    (version) => extensionVersion(version).version.split(".").map(Number),
  );
  for (let i = 1; i < versions.length; i++) {
    const differing = versions[i].findIndex(
      (part, index) => part !== versions[i - 1][index],
    );
    assert.ok(versions[i][differing] > versions[i - 1][differing]);
  }
});

test("unrepresentable release versions fail before packaging", () => {
  for (const version of [
    "",
    "1.2",
    "01.2.3",
    "1.2.3.4",
    "1.2.3-beta.1",
    "1.2.3+build",
    "1.2.3-rc.0",
    "1.2.3-rc.01",
    "1.2.3-rc.65535",
    "65536.0.0",
    "1.2.3-rc.999999999999999999",
  ])
    assert.throws(() => extensionVersion(version), undefined, version);
});

test("invalid signing keys fail without returning secret input", () => {
  const invalid = "private-content-that-must-not-appear-in-errors";
  assert.throws(
    () => signingPublicKey(invalid),
    (error) => !error.message.includes(invalid),
  );
  const ec = generateKeyPairSync("ec", { namedCurve: "prime256v1" });
  assert.throws(
    () =>
      signingPublicKey(ec.privateKey.export({ type: "pkcs8", format: "pem" })),
    /RSA/,
  );
});

test("malformed CRX headers are rejected", () => {
  assert.throws(() => verifyCrx3(Buffer.alloc(0)), /CRX3/);
  const malformed = Buffer.alloc(12);
  malformed.write("Cr24");
  malformed.writeUInt32LE(3, 4);
  malformed.writeUInt32LE(0xffffffff, 8);
  assert.throws(() => verifyCrx3(malformed), /header size/);
});

test(
  "Chrome produces signed CRX3 artifacts with a stable identity and matching manifests",
  {
    skip: !process.env.CHROME_BIN,
  },
  async () => {
    const directory = await mkdtemp(join(tmpdir(), "fjord-crx-test-"));
    try {
      const { privateKey } = generateKeyPairSync("rsa", {
        modulusLength: 2048,
      });
      const pem = privateKey.export({ type: "pkcs8", format: "pem" });
      const keyFile = join(directory, "signing.pem");
      await writeFile(keyFile, pem, { mode: 0o600 });
      const publicKey = signingPublicKey(pem);
      const candidate = await extensionPackage({
        version: "v1.2.3-rc.1",
        keyFile,
        chrome: process.env.CHROME_BIN,
      });
      const stable = await extensionPackage({
        version: "v1.2.3",
        keyFile,
        chrome: process.env.CHROME_BIN,
      });
      assert.match(stable.extensionId, /^[a-p]{32}$/);
      assert.equal(stable.extensionId, candidate.extensionId);
      assert.equal(await readFile(keyFile, "utf8"), pem);
      const verified = verifyCrx3(stable.crx, publicKey);
      assert.equal(verified.extensionId, stable.extensionId);
      const zipPath = join(directory, "unpacked.zip");
      const crxZipPath = join(directory, "crx.zip");
      await writeFile(zipPath, stable.zip);
      await writeFile(crxZipPath, verified.archive);
      const [zipManifest, crxManifest, entries] = await Promise.all([
        run("unzip", ["-p", zipPath, "manifest.json"]),
        run("unzip", ["-p", crxZipPath, "manifest.json"]),
        run("unzip", ["-Z1", crxZipPath]),
      ]);
      assert.deepEqual(
        JSON.parse(zipManifest.stdout),
        JSON.parse(crxManifest.stdout),
      );
      const manifest = JSON.parse(crxManifest.stdout);
      assert.equal(manifest.version, "1.2.3.65535");
      assert.equal(manifest.version_name, "1.2.3");
      assert.equal(manifest.key, publicKey.toString("base64"));
      assert.deepEqual(entries.stdout.trim().split("\n").sort(), [
        "LICENSE",
        "README.md",
        "manifest.json",
        "route.mjs",
        "rules.json",
        "service-worker.mjs",
      ]);
      const damaged = Buffer.from(stable.crx);
      damaged[damaged.length - 1] ^= 1;
      assert.throws(() => verifyCrx3(damaged, publicKey), /signature/);
      const other = generateKeyPairSync("rsa", {
        modulusLength: 2048,
      }).publicKey.export({ type: "spki", format: "der" });
      assert.throws(
        () => verifyCrx3(stable.crx, other),
        /configured extension key/,
      );
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  },
);
