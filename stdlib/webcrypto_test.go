package stdlib_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const webCryptoPrelude = `
const subtle = globalThis.crypto.subtle;
const encoder = new TextEncoder();
const check = (condition, label) => {
  if (!condition) throw new Error("WebCrypto acceptance: " + label);
};
const asBytes = value => value instanceof ArrayBuffer
  ? new Uint8Array(value)
  : new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
const hex = value => Array.from(asBytes(value), byte => byte.toString(16).padStart(2, "0")).join("");
const fromHex = value => Uint8Array.from(value.match(/../g) || [], byte => parseInt(byte, 16));
async function rejects(name, start, label) {
  let result;
  try {
    result = start();
  } catch (error) {
    throw new Error(label + " threw synchronously: " + error.name + ": " + error.message);
  }
  check(result && typeof result.then === "function", label + " did not return a Promise");
  const error = await result.then(() => null, reason => reason);
  check(error && error.name === name,
    label + " rejected with " + (error && error.name) + ", expected " + name);
  return error;
}
`

func executeWebCrypto(t *testing.T, profile stdlib.Profile, body string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{
		Name: "[WebCrypto acceptance]", Virtual: true, Module: true,
		Code: webCryptoPrelude + body,
	}, profile)
	if err != nil {
		t.Fatalf("WebCrypto execution failed: %s", stdlib.Diagnostic(err))
	}
	if result.Code != 0 {
		t.Fatalf("WebCrypto execution exited with %d: %+v", result.Code, result.Diagnostics)
	}
}

func TestWebCryptoDigestAndHMACVectors(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
if (typeof fetch !== "undefined") throw Error("crypto selected Fetch");
if (await import("noe:fs").then(() => true, () => false)) throw Error("crypto selected FS");

import nodeDefault, {webcrypto as nodeWebcrypto, subtle as nodeSubtle} from "node:crypto";
import cryptoDefault, {webcrypto as aliasWebcrypto, subtle as aliasSubtle} from "crypto";
check(nodeWebcrypto === globalThis.crypto && aliasWebcrypto === globalThis.crypto,
  "Node webcrypto exports must retain global Crypto identity");
check(nodeSubtle === subtle && aliasSubtle === subtle &&
  nodeDefault.webcrypto === globalThis.crypto && cryptoDefault.subtle === subtle,
  "Node subtle exports must retain global SubtleCrypto identity");

const data = encoder.encode("abc");
const digests = [
  ["SHA-1", "a9993e364706816aba3e25717850c26c9cd0d89d"],
  ["SHA-256", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"],
  ["SHA-384", "cb00753f45a35e8bb5a03d699ac65007272c32ab0eded1631a8b605a43ff5bed8086072ba1e7cc2358baeca134c825a7"],
  ["SHA-512", "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"],
];
for (const [name, expected] of digests) {
  check(hex(await subtle.digest(name.toLowerCase(), data)) === expected, name + " digest vector");
}

const message = encoder.encode("what do ya want for nothing?");
const hmacs = [
  ["SHA-1", "effcdf6ae5eb2fa2d27416d5f184df9c259a7c79"],
  ["SHA-256", "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"],
  ["SHA-384", "af45d2e376484031617f78d2b58a6b1b9c7ef464f5a01b47e42ec3736322445e8e2240ca5e69e2c78b3239ecfab21649"],
  ["SHA-512", "164b7a7bfcf819e2e395fbe73b56e0a387bd64222e831fd610270cd7ea2505549758bf75c05a994a6d034f65f8f0e6fdcaeab1a34d4a6b4b636e070a38bce737"],
];
for (const [hash, expected] of hmacs) {
  const key = await subtle.importKey("raw", encoder.encode("Jefe"),
    {name: "HMAC", hash: {name: hash.toLowerCase()}}, true, ["sign", "verify"]);
  check(key.algorithm.name === "HMAC" && key.algorithm.hash.name === hash,
    hash + " HMAC algorithm metadata");
  const signature = await subtle.sign("HMAC", key, message);
  check(signature instanceof ArrayBuffer && hex(signature) === expected, hash + " HMAC vector");
  check(await subtle.verify("HMAC", key, signature, message), hash + " HMAC verify vector");
  check(!(await subtle.verify("HMAC", key, signature, encoder.encode("modified"))),
    hash + " HMAC modified message");
  const changed = new Uint8Array(signature.slice(0));
  changed[0] ^= 1;
  check(!(await subtle.verify("HMAC", key, changed, message)), hash + " HMAC modified signature");
}
`)
}

func TestWebCryptoSignOnlyKeyCannotVerify(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const keyBytes = encoder.encode("Jefe");
const message = encoder.encode("what do ya want for nothing?");
const expected = fromHex("5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843");
const params = {name: "HMAC", hash: {name: "SHA-256"}};
const signUsages = ["sign"];
const signOnly = await subtle.importKey("raw", keyBytes, params, true, signUsages);
const signed = await subtle.sign("HMAC", signOnly, message);
check(hex(signed) === "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843",
  "sign-only key can sign");
await rejects("InvalidAccessError", () => subtle.verify("HMAC", signOnly, expected, message),
  "sign-only key cannot verify");
`)
}

func TestWebCryptoVerifyOnlyKeyCannotSign(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const keyBytes = encoder.encode("Jefe");
const message = encoder.encode("what do ya want for nothing?");
const expected = fromHex("5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843");
const verifyOnly = await subtle.importKey("raw", keyBytes,
  {name: "HMAC", hash: {name: "SHA-256"}}, true, ["verify"]);
check(await subtle.verify("HMAC", verifyOnly, expected, message),
  "verify-only key verifies without sign permission");
await rejects("InvalidAccessError", () => subtle.sign("HMAC", verifyOnly, message),
  "verify-only key cannot sign");
`)
}

func TestWebCryptoImportSnapshotsCallerInputs(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const keyBytes = encoder.encode("Jefe");
const verifyUsages = ["verify"];
const importParams = {name: "HMAC", hash: {name: "SHA-256"}};
const pendingKey = subtle.importKey("raw", keyBytes, importParams, false, verifyUsages);
verifyUsages.length = 0;
importParams.name = "PBKDF2";
importParams.hash.name = "SHA-1";
keyBytes.fill(0);
const verifyOnly = await pendingKey;
check(verifyOnly.type === "secret" && verifyOnly.extractable === false &&
  verifyOnly.algorithm.name === "HMAC" && verifyOnly.algorithm.hash.name === "SHA-256" &&
  verifyOnly.usages.length === 1 && verifyOnly.usages[0] === "verify",
  "importKey snapshots raw bytes, algorithm dictionaries, and usage arrays");
check(await subtle.verify("HMAC", verifyOnly,
  fromHex("5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"),
  encoder.encode("what do ya want for nothing?")), "imported key retained its snapshotted bytes");
`)
}

func TestWebCryptoExtractabilityUsesPrivateState(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const key = await subtle.importKey("raw", encoder.encode("secret"),
  {name: "HMAC", hash: {name: "SHA-256"}}, false, ["sign"]);
check(key.extractable === false, "non-extractable key metadata");
Object.defineProperty(key, "extractable", {value: true, configurable: true});
check(key.extractable === true, "the public property can be shadowed");
await rejects("InvalidAccessError", () => subtle.exportKey("raw", key),
  "shadowing extractable does not grant export access");
`)
}

func TestWebCryptoMetadataMutationsDoNotChangeKeyAuthority(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const message = encoder.encode("what do ya want for nothing?");
const expected = fromHex("5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843");
const key = await subtle.importKey("raw", encoder.encode("Jefe"),
  {name: "HMAC", hash: {name: "SHA-256"}}, false, ["verify"]);
const algorithm = key.algorithm;
const usages = key.usages;
check(key.algorithm === algorithm && key.usages === usages,
  "CryptoKey metadata getters cache their visible objects");
algorithm.name = "PBKDF2";
algorithm.hash.name = "SHA-1";
Object.defineProperty(key, "usages", {value: ["sign"], configurable: true});
check(key.usages[0] === "sign", "visible usages can be shadowed");
check(await subtle.verify("HMAC", key, expected, message),
  "visible algorithm and usage tampering do not change private verify authority");
await rejects("InvalidAccessError", () => subtle.sign("HMAC", key, message),
  "shadowed visible usages do not grant sign authority");
`)
}

func TestWebCryptoCryptoKeyBrandAndReadonlyAttributes(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const verifyOnly = await subtle.importKey("raw", encoder.encode("Jefe"),
  {name: "HMAC", hash: {name: "SHA-256"}}, false, ["verify"]);
const keyPrototype = CryptoKey.prototype;
check(typeof CryptoKey === "function", "CryptoKey interface object is exposed");
try { new CryptoKey(); throw Error("CryptoKey constructor unexpectedly succeeded"); }
catch (error) { check(error.name === "TypeError", "CryptoKey constructor is illegal"); }
for (const property of ["type", "extractable", "algorithm", "usages"]) {
  const descriptor = Object.getOwnPropertyDescriptor(keyPrototype, property);
  check(descriptor && typeof descriptor.get === "function" && descriptor.set === undefined,
    "CryptoKey." + property + " is a branded readonly attribute");
  check(Reflect.set(verifyOnly, property, null) === false,
    "CryptoKey." + property + " rejects assignment");
  let brandError;
  try { descriptor.get.call({}); } catch (error) { brandError = error; }
  check(brandError && brandError.name === "TypeError",
    "CryptoKey." + property + " getter checks its brand");
}
await rejects("TypeError", () => subtle.exportKey("raw", {}), "exportKey requires a branded CryptoKey");
`)
}

func TestWebCryptoCryptoKeyCloneIsBrandedOrRejected(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const key = await subtle.importKey("raw", encoder.encode("Jefe"),
  {name: "HMAC", hash: {name: "SHA-256"}}, true, ["sign"]);
const message = encoder.encode("what do ya want for nothing?");
let clone;
let cloneError;
try { clone = structuredClone(key); } catch (error) { cloneError = error; }
if (cloneError) {
  check(cloneError.name === "DataCloneError", "unsupported CryptoKey cloning reports DataCloneError");
} else {
  check(clone instanceof CryptoKey && clone !== key,
    "a cloned CryptoKey stays branded instead of degrading to metadata");
  check(hex(await subtle.sign("HMAC", clone, message)) ===
    "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843",
    "a supported cloned CryptoKey retains key material and usage state");
}

`)
}

func TestWebCryptoMetadataDoesNotInvokePrototypeHooks(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const key = await subtle.importKey("raw", encoder.encode("Jefe"),
  {name: "HMAC", hash: "SHA-256"}, false, ["verify"]);
const define = Object.defineProperty;
const objectPrototype = Object.prototype;
const arrayPrototype = Array.prototype;
const cacheDescriptor = Object.create(null);
cacheDescriptor.configurable = true;
cacheDescriptor.get = () => { throw Error("metadata accessed an inherited cache getter"); };
cacheDescriptor.set = () => { throw Error("metadata accessed an inherited cache setter"); };
const indexDescriptor = Object.create(null);
indexDescriptor.configurable = true;
indexDescriptor.set = () => { throw Error("metadata copy invoked an inherited index setter"); };
try {
  define(objectPrototype, "algorithmCached", cacheDescriptor);
  define(objectPrototype, "usagesCached", cacheDescriptor);
  define(arrayPrototype, "0", indexDescriptor);
  const algorithm = key.algorithm;
  const usages = key.usages;
  check(algorithm.name === "HMAC" && algorithm.hash.name === "SHA-256" &&
    usages.length === 1 && usages[0] === "verify", "metadata remains readable without prototype callbacks");
} finally {
  delete objectPrototype.algorithmCached;
  delete objectPrototype.usagesCached;
  delete arrayPrototype[0];
}
await rejects("InvalidAccessError", () => subtle.sign("HMAC", key, encoder.encode("x")),
  "metadata reads preserve the key's usage policy");
`)
}

func TestWebCryptoDeriveKeyChecksAccessBeforeTargetLength(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const material = encoder.encode("password");
const params = {name: "PBKDF2", salt: encoder.encode("salt"), iterations: 1, hash: "SHA-256"};
const target = {name: "HMAC", hash: "SHA-256", length: 0};
const bitsOnly = await subtle.importKey("raw", material, "PBKDF2", false, ["deriveBits"]);
const wrongAlgorithm = await subtle.importKey("raw", material, "HKDF", false, ["deriveKey"]);
for (const [key, label] of [[bitsOnly, "missing deriveKey usage"], [wrongAlgorithm, "mismatched base algorithm"]]) {
  await rejects("InvalidAccessError", () => subtle.deriveKey(params, key, target, false, ["sign"]),
    label + " is checked before the target key length");
}
const permitted = await subtle.importKey("raw", material, "PBKDF2", false, ["deriveKey"]);
await rejects("TypeError", () => subtle.deriveKey(params, permitted, target, false, ["sign"]),
  "zero HMAC target length is still rejected after access checks pass");
`)
}

func TestWebCryptoDerivationVectorsAndConstraints(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const password = encoder.encode("password");
const saltBytes = encoder.encode("salt");
const pbkdf2 = await subtle.importKey("raw", password, "PBKDF2", false, ["deriveBits", "deriveKey"]);
check(pbkdf2.extractable === false && pbkdf2.algorithm.name === "PBKDF2" &&
  pbkdf2.usages.join(",") === "deriveKey,deriveBits", "PBKDF2 import metadata");
const pbkdf2SHA1 = await subtle.deriveBits(
  {name: "PBKDF2", salt: saltBytes, iterations: 1, hash: "SHA-1"}, pbkdf2, 160);
check(hex(pbkdf2SHA1) === "0c60c80f961f0e71f3a9b524af6012062fe037a6", "RFC 6070 PBKDF2-SHA1 vector");
const pbkdf2SHA256 = await subtle.deriveBits(
  {name: "PBKDF2", salt: saltBytes, iterations: 1, hash: "SHA-256"}, pbkdf2, 256);
check(hex(pbkdf2SHA256) === "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b",
  "PBKDF2-SHA256 vector");
const snapSalt = encoder.encode("salt");
const snapHash = {name: "SHA-256"};
const snapParams = {name: "PBKDF2", salt: snapSalt, iterations: 1, hash: snapHash};
const pendingPBKDF2 = subtle.deriveBits(snapParams, pbkdf2, 256);
snapSalt.fill(0); snapParams.salt = encoder.encode("changed");
snapParams.iterations = 2; snapHash.name = "SHA-512";
check(hex(await pendingPBKDF2) === "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b",
  "deriveBits snapshots caller salt and dictionary values before asynchronous work");

const ikm = new Uint8Array(22).fill(0x0b);
const hkdfSalt = Uint8Array.from({length: 13}, (_, index) => index);
const info = Uint8Array.from({length: 10}, (_, index) => 0xf0 + index);
const hkdf = await subtle.importKey("raw", ikm, "HKDF", false, ["deriveBits", "deriveKey"]);
check(hkdf.extractable === false && hkdf.algorithm.name === "HKDF" &&
  hkdf.usages.join(",") === "deriveKey,deriveBits", "HKDF import metadata");
const hkdfParams = {name: "HKDF", hash: {name: "SHA-256"}, salt: hkdfSalt, info};
const hkdfBits = await subtle.deriveBits(hkdfParams, hkdf, 336);
const hkdfVector = "3cb25f25faacd57a90434f64d0362f2a2d2d0a90cf1a5a4c5db02d56ecc4c5bf34007208d5b887185865";
check(hex(hkdfBits) === hkdfVector, "RFC 5869 HKDF-SHA256 vector");

const derivedHKDF = await subtle.deriveKey(hkdfParams, hkdf,
  {name: "HMAC", hash: {name: "SHA-256"}, length: 256}, true, ["sign"]);
check(derivedHKDF.algorithm.name === "HMAC" && derivedHKDF.algorithm.hash.name === "SHA-256" &&
  derivedHKDF.algorithm.length === 256 && derivedHKDF.extractable === true &&
  derivedHKDF.usages.join(",") === "sign", "HKDF deriveKey applies HMAC import rules and requested metadata");
check(hex(await subtle.exportKey("raw", derivedHKDF)) === hkdfVector.slice(0, 64),
  "HKDF deriveKey matches the first 256 RFC vector bits");

const pbkdf2Derived = await subtle.deriveKey(
  {name: "PBKDF2", salt: saltBytes, iterations: 1, hash: "SHA-256"}, pbkdf2,
  {name: "HMAC", hash: "SHA-256", length: 256}, true, ["sign"]);
check(hex(await subtle.exportKey("raw", pbkdf2Derived)) ===
  "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b",
  "PBKDF2 deriveKey matches the PBKDF2 vector");

const hkdfDeriveKeyOnly = await subtle.importKey("raw", ikm, "HKDF", false, ["deriveKey"]);
for (const [hash, defaultLength] of [["SHA-256", 512], ["SHA-512", 1024]]) {
  const key = await subtle.deriveKey(hkdfParams, hkdfDeriveKeyOnly,
    {name: "HMAC", hash}, false, ["sign"]);
  check(key.algorithm.length === defaultLength && key.extractable === false &&
    key.usages.join(",") === "sign", hash + " HMAC default is the hash block size");
  check((await subtle.sign("HMAC", key, encoder.encode("key-length check"))).byteLength ===
    (defaultLength / 8 === 64 ? 32 : 64), hash + " HMAC derived key is usable");
}

const bitsOnly = await subtle.importKey("raw", ikm, "HKDF", false, ["deriveBits"]);
await rejects("InvalidAccessError", () => subtle.deriveKey(hkdfParams, bitsOnly,
  {name: "HMAC", hash: "SHA-256", length: 256}, false, ["sign"]),
  "deriveKey does not accept deriveBits-only authority");
await rejects("NotSupportedError", () => subtle.exportKey("raw", hkdf),
  "HKDF has no export operation; specification checks this before extractability");

await rejects("OperationError", () => subtle.deriveBits(
  {name: "PBKDF2", salt: saltBytes, iterations: 1, hash: "SHA-256"}, pbkdf2, 7),
  "PBKDF2 length must be a multiple of eight bits");
await rejects("OperationError", () => subtle.deriveBits(
  {name: "PBKDF2", salt: saltBytes, iterations: 0, hash: "SHA-256"}, pbkdf2, 8),
  "PBKDF2 requires a positive iteration count");
check((await subtle.deriveBits(
  {name: "PBKDF2", salt: saltBytes, iterations: 1, hash: "SHA-256"}, pbkdf2, 0)).byteLength === 0,
  "current editor draft permits zero-bit PBKDF2 output after iteration validation");
await rejects("OperationError", () => subtle.deriveBits(
  {name: "PBKDF2", salt: saltBytes, iterations: 0, hash: "SHA-256"}, pbkdf2, 0),
  "zero-bit PBKDF2 still requires positive iterations");
await rejects("OperationError", () => subtle.deriveBits(
  {name: "HKDF", hash: "SHA-256", salt: hkdfSalt, info}, hkdf, 8161 * 8),
  "HKDF enforces the RFC 5869 output limit");
`)
}

func TestWebCryptoBufferSourcesAndSnapshots(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const expectedDigest = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad";
const source = Uint8Array.from([0xaa, 0x61, 0x62, 0x63, 0xbb]);
const view = new DataView(source.buffer, 1, 3);
const digestPromise = subtle.digest({name: "SHA-256"}, view);
source.fill(0x78);
check(hex(await digestPromise) === expectedDigest,
  "DataView exact offset and bytes are copied before digest work yields");

const arrayBuffer = encoder.encode("abc").buffer;
check(hex(await subtle.digest("SHA-256", arrayBuffer)) === expectedDigest,
  "ArrayBuffer is accepted as a BufferSource");
const surrounding = Uint8Array.from([0, 0x61, 0x62, 0x63, 0xff]);
check(hex(await subtle.digest("SHA-256", surrounding.subarray(1, 4))) === expectedDigest,
  "typed-array byteOffset and byteLength are honored");

const keyBacking = Uint8Array.from([0, 0x4a, 0x65, 0x66, 0x65, 0xff]);
const keyView = new DataView(keyBacking.buffer, 1, 4);
const keyPromise = subtle.importKey("raw", keyView,
  {name: "HMAC", hash: {name: "SHA-256"}}, true, ["sign", "verify"]);
keyBacking.fill(0);
const key = await keyPromise;
check(hex(await subtle.exportKey("raw", key)) === "4a656665",
  "DataView key import snapshots only its selected bytes before yielding");

const payload = encoder.encode("what do ya want for nothing?");
const signedBacking = new Uint8Array(payload.length + 2);
signedBacking.set(payload, 1);
const payloadView = new DataView(signedBacking.buffer, 1, payload.length);
const signaturePromise = subtle.sign("HMAC", key, payloadView);
signedBacking.fill(0);
const signature = await signaturePromise;
check(hex(signature) === "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843",
  "DataView sign snapshots exact bytes before asynchronous calculation");

const signatureBacking = new Uint8Array(asBytes(signature).length + 2);
signatureBacking.set(asBytes(signature), 1);
check(await subtle.verify("HMAC", key,
  new DataView(signatureBacking.buffer, 1, asBytes(signature).length), payload),
  "DataView signature conversion honors its exact byte range");

for (const invalid of [encoder.encode("abc").join(",").split(",").map(Number), "abc", {0: 97, 1: 98, 2: 99}]) {
  await rejects("TypeError", () => subtle.digest("SHA-256", invalid),
    "digest rejects non-BufferSource values");
}
await rejects("TypeError", () => subtle.sign("HMAC", key, "abc"),
  "sign rejects string data instead of coercing it");

const shared = new Uint8Array(new SharedArrayBuffer(3));
await rejects("TypeError", () => subtle.digest("SHA-256", shared),
  "WebIDL BufferSource rejects SharedArrayBuffer-backed input");
const detachedBuffer = new ArrayBuffer(3);
const detachedView = new Uint8Array(detachedBuffer);
structuredClone(detachedBuffer, {transfer: [detachedBuffer]});
await rejects("TypeError", () => subtle.digest("SHA-256", detachedView),
  "WebIDL BufferSource rejects a detached ArrayBuffer view");
`)
}

func TestWebCryptoValidationErrorsAndRequiredDictionaries(t *testing.T) {
	executeWebCrypto(t, stdlib.Profile{}, `
const data = encoder.encode("x");
const hmacData = encoder.encode("Jefe");
const validHMAC = {name: "HMAC", hash: "SHA-256"};
const hmacKey = await subtle.importKey("raw", hmacData, validHMAC, true, ["sign"]);
const pbkdf2 = await subtle.importKey("raw", data, "PBKDF2", false, ["deriveBits"]);
const hkdf = await subtle.importKey("raw", data, "HKDF", false, ["deriveBits"]);

await rejects("TypeError", () => subtle.digest(), "required digest arguments are rejected by Promise");
await rejects("TypeError", () => subtle.importKey(), "required importKey arguments are rejected by Promise");
await rejects("TypeError", () => subtle.sign(), "required sign arguments are rejected by Promise");
await rejects("TypeError", () => subtle.digest("SHA-256", "x"), "string is not a BufferSource");
await rejects("TypeError", () => subtle.importKey("raw", [1, 2], validHMAC, true, ["sign"]),
  "raw import rejects ordinary arrays");
await rejects("TypeError", () => subtle.importKey("RAW", hmacData, validHMAC, true, ["sign"]),
  "KeyFormat enum spelling is case-sensitive");
await rejects("NotSupportedError", () => subtle.digest("MD5", data),
  "Web digest rejects an unregistered Node hash");
await rejects("NotSupportedError", () => subtle.digest("SHA256", data),
  "Web digest rejects a missing-hyphen alias");
await rejects("NotSupportedError", () => subtle.importKey("pkcs8", hmacData, validHMAC, true, ["sign"]),
  "HMAC raw-only import rejects PKCS8");
await rejects("NotSupportedError", () => subtle.importKey("raw", hmacData,
  {name: "HMAC", hash: "MD5"}, true, ["sign"]), "HMAC rejects an unregistered hash");
await rejects("TypeError", () => subtle.importKey("raw", hmacData,
  {name: "HMAC"}, true, ["sign"]), "HMAC import requires hash");
await rejects("DataError", () => subtle.importKey("raw", new Uint8Array(0), validHMAC, true, ["sign"]),
  "HMAC import rejects an empty key");
await rejects("DataError", () => subtle.importKey("raw", hmacData,
  {name: "HMAC", hash: "SHA-256", length: 40}, true, ["sign"]),
  "HMAC import rejects a length larger than raw key material");
await rejects("SyntaxError", () => subtle.importKey("raw", hmacData, validHMAC, true, ["deriveBits"]),
  "HMAC import accepts only sign and verify usages");
await rejects("TypeError", () => subtle.sign("HMAC", {}, data), "sign requires a branded CryptoKey");
await rejects("InvalidAccessError", () => subtle.verify("HMAC", hmacKey,
  new Uint8Array(32), data), "verify requires verify usage");

await rejects("SyntaxError", () => subtle.importKey("raw", data, "PBKDF2", true, ["deriveBits"]),
  "PBKDF2 raw key must be non-extractable");
await rejects("SyntaxError", () => subtle.importKey("raw", data, "PBKDF2", false, ["sign"]),
  "PBKDF2 allows only derivation usages");
await rejects("SyntaxError", () => subtle.importKey("raw", data, "HKDF", true, ["deriveBits"]),
  "HKDF raw key must be non-extractable");
await rejects("SyntaxError", () => subtle.importKey("raw", data, "HKDF", false, ["verify"]),
  "HKDF allows only derivation usages");
for (const algorithm of [validHMAC, "PBKDF2", "HKDF"]) {
  await rejects("SyntaxError", () => subtle.importKey("raw", hmacData, algorithm, false, []),
    "importKey rejects empty usages for secret keys");
}
await rejects("TypeError", () => subtle.deriveBits({name: "PBKDF2", salt: data, iterations: 1}, pbkdf2, 8),
  "PBKDF2 requires a hash dictionary member");
await rejects("TypeError", () => subtle.deriveBits({name: "PBKDF2", iterations: 1, hash: "SHA-256"}, pbkdf2, 8),
  "PBKDF2 requires salt");
await rejects("TypeError", () => subtle.deriveBits({name: "PBKDF2", salt: data, hash: "SHA-256"}, pbkdf2, 8),
  "PBKDF2 requires iterations");
await rejects("TypeError", () => subtle.deriveBits({name: "HKDF", salt: data, hash: "SHA-256"}, hkdf, 8),
  "HKDF requires info");
await rejects("TypeError", () => subtle.deriveBits({name: "HKDF", info: data, hash: "SHA-256"}, hkdf, 8),
  "HKDF requires salt");
`)
}

func TestWebCryptoMainAndWorkerProfiles(t *testing.T) {
	worker := `import {parentPort} from "node:worker_threads";
if (typeof fetch !== "undefined") throw Error("Worker gained Fetch");
if (await import("noe:fs").then(() => true, () => false)) throw Error("Worker gained FS");
const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode("abc"));
if (Array.from(new Uint8Array(digest), x => x.toString(16).padStart(2, "0")).join("") !==
  "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad") throw Error("Worker digest");
const base = await crypto.subtle.importKey("raw", new TextEncoder().encode("password"),
  "PBKDF2", false, ["deriveBits"]);
const bits = await crypto.subtle.deriveBits({name: "PBKDF2", salt: new TextEncoder().encode("salt"),
  iterations: 1, hash: "SHA-256"}, base, 256);
if (new Uint8Array(bits).length !== 32) throw Error("Worker PBKDF2");
parentPort.postMessage("worker WebCrypto passed"); parentPort.close();`
	code := fmt.Sprintf(`
if (typeof fetch !== "undefined") throw Error("main gained Fetch");
if (await import("noe:fs").then(() => true, () => false)) throw Error("main gained FS");
if (hex(await crypto.subtle.digest("SHA-256", encoder.encode("abc"))) !==
  "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad") throw Error("main digest");
import {Worker} from "node:worker_threads";
const worker = new Worker(%q, {eval: true, type: "module"});
const message = await new Promise((resolve, reject) => {
  worker.once("message", resolve); worker.once("error", reject);
});
await worker.terminate();
check(message === "worker WebCrypto passed", "Worker runs crypto with only Worker selection");
`, worker)
	executeWebCrypto(t, stdlib.Profile{Modules: stdlib.Modules{Workers: &stdlib.WorkerOptions{Max: 1}}}, code)
}
