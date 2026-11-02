import {
  chmod,
  readFile,
  mkdir,
  mkdtemp,
  rm,
  utimes,
  writeFile,
} from "node:fs/promises";
import { execFile } from "node:child_process";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { parseArgs } from "node:util";
import { extensionVersion, signingPublicKey, verifyCrx3 } from "./crx.mjs";

const root = dirname(fileURLToPath(import.meta.url));
const files = [
  "manifest.json",
  "rules.json",
  "route.mjs",
  "service-worker.mjs",
  "README.md",
  "LICENSE",
];
const run = promisify(execFile);

export async function extensionPackage({
  version,
  keyFile,
  chrome = process.env.CHROME_BIN || "google-chrome",
} = {}) {
  const releaseVersion = version ? extensionVersion(version) : undefined;
  const publicKey = keyFile
    ? signingPublicKey(await readFile(keyFile))
    : undefined;
  const directory = await mkdtemp(join(tmpdir(), "fjord-extension-"));
  try {
    const source = join(directory, "unpacked");
    await mkdir(source);
    const timestamp = new Date("1980-01-01T00:00:00.000Z");
    for (const name of files) {
      let content = await readFile(
        resolve(root, name === "LICENSE" ? "../LICENSE" : name),
      );
      if (name === "manifest.json" && (releaseVersion || publicKey))
        content = Buffer.from(
          `${JSON.stringify(
            {
              ...JSON.parse(content),
              ...releaseVersion,
              ...(publicKey ? { key: publicKey.toString("base64") } : {}),
            },
            null,
            2,
          )}\n`,
        );
      const destination = join(source, name);
      await writeFile(destination, content);
      await chmod(destination, 0o644);
      await utimes(destination, timestamp, timestamp);
    }
    const zip = join(directory, "extension.zip");
    await run("zip", ["-X", "-q", zip, ...files], {
      cwd: source,
      env: { ...process.env, TZ: "UTC" },
    });
    const result = { zip: await readFile(zip) };
    if (keyFile) {
      try {
        await run(
          chrome,
          [
            "--headless=new",
            "--no-first-run",
            "--no-default-browser-check",
            "--disable-background-networking",
            `--user-data-dir=${join(directory, "profile")}`,
            `--pack-extension=${source}`,
            `--pack-extension-key=${resolve(keyFile)}`,
          ],
          {
            timeout: 60000,
            maxBuffer: 1024 * 1024,
            env: {
              PATH: process.env.PATH,
              HOME: directory,
              TMPDIR: directory,
              LANG: "C",
            },
          },
        );
      } catch {
        throw new Error(
          "Chrome could not package the extension. Check CHROME_BIN and the signing-key file.",
        );
      }
      result.crx = await readFile(join(directory, "unpacked.crx"));
      result.extensionId = verifyCrx3(result.crx, publicKey).extensionId;
    }
    return result;
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}

export async function extensionArchive(options) {
  return (await extensionPackage(options)).zip;
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const { values, positionals } = parseArgs({
    allowPositionals: true,
    options: {
      version: { type: "string" },
      crx: { type: "string" },
      "key-file": { type: "string" },
    },
  });
  if (positionals.length > 1) throw new Error("Expected one ZIP destination.");
  const keyFile = values["key-file"] || process.env.FJORD_EXTENSION_KEY_FILE;
  if (values.crx && !keyFile)
    throw new Error(
      "CRX packaging requires FJORD_EXTENSION_KEY_FILE or --key-file.",
    );
  if (values.crx && !values.version)
    throw new Error("CRX packaging requires an explicit release --version.");
  const destination = resolve(
    positionals[0] || "artifacts/fjord-chrome-extension.zip",
  );
  const crx = values.crx ? resolve(values.crx) : undefined;
  if (crx === destination)
    throw new Error("ZIP and CRX destinations must differ.");
  if (keyFile && [destination, crx].includes(resolve(keyFile)))
    throw new Error("An extension artifact cannot overwrite its signing key.");
  const result = await extensionPackage({
    version: values.version,
    keyFile: values.crx ? keyFile : undefined,
  });
  await mkdir(dirname(destination), { recursive: true });
  await writeFile(destination, result.zip);
  process.stdout.write(`${destination}\n`);
  if (crx) {
    await mkdir(dirname(crx), { recursive: true });
    await writeFile(crx, result.crx);
    process.stdout.write(`${crx}\nExtension ID: ${result.extensionId}\n`);
  }
}
