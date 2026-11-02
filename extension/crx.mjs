import {
  constants,
  createHash,
  createPrivateKey,
  createPublicKey,
  verify,
} from "node:crypto";

export function extensionVersion(version) {
  const match =
    /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc\.([1-9]\d*))?$/.exec(
      version,
    );
  if (!match) throw new Error("Extension version must be X.Y.Z or X.Y.Z-rc.N.");
  const parts = match.slice(1, 4).map(Number);
  const revision = match[4] === undefined ? 65535 : Number(match[4]);
  if (
    parts.some((part) => !Number.isSafeInteger(part) || part > 65535) ||
    !Number.isSafeInteger(revision) ||
    revision > (match[4] === undefined ? 65535 : 65534)
  )
    throw new Error("Chrome version components exceed the supported range.");
  return {
    version: [...parts, revision].join("."),
    version_name: version.replace(/^v/, ""),
  };
}

export function signingPublicKey(pem) {
  let key;
  try {
    key = createPrivateKey(pem);
  } catch {
    throw new Error(
      "The extension signing key must be an unencrypted RSA PEM private key.",
    );
  }
  if (
    key.asymmetricKeyType !== "rsa" ||
    key.asymmetricKeyDetails.modulusLength < 2048
  )
    throw new Error(
      "The extension signing key must be RSA with at least 2048 bits.",
    );
  return createPublicKey(key).export({ format: "der", type: "spki" });
}

function fields(buffer) {
  const result = new Map();
  let offset = 0;
  function varint() {
    let value = 0;
    let multiplier = 1;
    for (let index = 0; index < 5; index++) {
      if (offset >= buffer.length) throw new Error("Truncated CRX3 header.");
      const byte = buffer[offset++];
      value += (byte & 127) * multiplier;
      if (!(byte & 128)) return value;
      multiplier *= 128;
    }
    throw new Error("Invalid CRX3 header integer.");
  }
  while (offset < buffer.length) {
    const tag = varint();
    if ((tag & 7) !== 2 || tag < 8)
      throw new Error("Unsupported CRX3 header field.");
    const size = varint();
    if (size > buffer.length - offset) throw new Error("Truncated CRX3 field.");
    const number = tag >>> 3;
    if (result.has(number)) throw new Error("Duplicate CRX3 header field.");
    result.set(number, buffer.subarray(offset, offset + size));
    offset += size;
  }
  return result;
}

// Verify Chrome's output against its documented CRX3 signed-header format.
// Signing remains Chrome's job; this catches a wrong key or damaged artifact.
export function verifyCrx3(crx, expectedPublicKey) {
  if (
    crx.length < 12 ||
    crx.subarray(0, 4).toString() !== "Cr24" ||
    crx.readUInt32LE(4) !== 3
  )
    throw new Error("Expected a CRX3 extension.");
  const headerSize = crx.readUInt32LE(8);
  if (headerSize > 1024 * 1024 || headerSize > crx.length - 12)
    throw new Error("Invalid CRX3 header size.");
  const header = fields(crx.subarray(12, 12 + headerSize));
  const signed = header.get(10000);
  const proof = header.get(2);
  if (!signed || !proof) throw new Error("Missing CRX3 developer proof.");
  const keyProof = fields(proof);
  const publicKey = keyProof.get(1);
  const signature = keyProof.get(2);
  const id = fields(signed).get(1);
  if (!publicKey || !signature || id?.length !== 16)
    throw new Error("Incomplete CRX3 developer proof.");
  if (expectedPublicKey && !publicKey.equals(expectedPublicKey))
    throw new Error("CRX3 was not signed by the configured extension key.");
  if (
    !createHash("sha256").update(publicKey).digest().subarray(0, 16).equals(id)
  )
    throw new Error("CRX3 extension ID does not match its signing key.");
  const archive = crx.subarray(12 + headerSize);
  if (archive.length < 4 || archive.readUInt32LE(0) !== 0x04034b50)
    throw new Error("CRX3 does not contain a ZIP archive.");
  const signedSize = Buffer.alloc(4);
  signedSize.writeUInt32LE(signed.length);
  const payload = Buffer.concat([
    Buffer.from("CRX3 SignedData\0"),
    signedSize,
    signed,
    archive,
  ]);
  const key = createPublicKey({ key: publicKey, format: "der", type: "spki" });
  if (
    !verify(
      "sha256",
      payload,
      { key, padding: constants.RSA_PKCS1_PADDING },
      signature,
    )
  )
    throw new Error("CRX3 signature verification failed.");
  return {
    extensionId: [...id.toString("hex")]
      .map((digit) => String.fromCharCode(97 + parseInt(digit, 16)))
      .join(""),
    archive,
    publicKey,
  };
}
