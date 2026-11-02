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

export async function extensionArchive() {
  const directory = await mkdtemp(join(tmpdir(), "fjord-extension-"));
  try {
    const timestamp = new Date("1980-01-01T00:00:00.000Z");
    for (const name of files) {
      const content = await readFile(
        resolve(root, name === "LICENSE" ? "../LICENSE" : name),
      );
      const destination = join(directory, name);
      await writeFile(destination, content);
      await chmod(destination, 0o644);
      await utimes(destination, timestamp, timestamp);
    }
    await run("zip", ["-X", "-q", "extension.zip", ...files], {
      cwd: directory,
      env: { ...process.env, TZ: "UTC" },
    });
    return await readFile(join(directory, "extension.zip"));
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const destination = resolve(
    process.argv[2] || "artifacts/fjord-chrome-extension.zip",
  );
  await mkdir(dirname(destination), { recursive: true });
  await writeFile(destination, await extensionArchive());
  process.stdout.write(`${destination}\n`);
}
