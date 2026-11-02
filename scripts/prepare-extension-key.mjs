import { createPrivateKey, generateKeyPairSync } from "node:crypto";
import { appendFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";

const temporary = process.env.RUNNER_TEMP;
if (!temporary) throw new Error("RUNNER_TEMP is required");
const path = join(temporary, "fjord-extension-signing.pem");
if (process.argv[2] === "--cleanup") {
  await rm(path, { force: true });
} else {
  if (!process.env.GITHUB_ENV) throw new Error("GITHUB_ENV is required");
  const dryRun = process.env.RELEASE_DRY_RUN === "true";
  const pem = dryRun
    ? generateKeyPairSync("rsa", {
        modulusLength: 2048,
        privateKeyEncoding: { type: "pkcs8", format: "pem" },
        publicKeyEncoding: { type: "spki", format: "pem" },
      }).privateKey
    : process.env.FJORD_EXTENSION_SIGNING_KEY;
  if (!pem) {
    throw new Error("Configure FJORD_EXTENSION_SIGNING_KEY before publishing");
  }
  let key;
  try {
    key = createPrivateKey(pem);
  } catch {
    throw new Error("FJORD_EXTENSION_SIGNING_KEY must be a valid RSA PEM key");
  }
  if (key.asymmetricKeyType !== "rsa" ||
      (key.asymmetricKeyDetails?.modulusLength ?? 0) < 2048) {
    throw new Error("Extension signing requires an RSA key of at least 2048 bits");
  }
  await writeFile(path, pem, { mode: 0o600, flag: "wx" });
  await appendFile(process.env.GITHUB_ENV, `FJORD_EXTENSION_KEY_FILE=${path}\n`);
}
