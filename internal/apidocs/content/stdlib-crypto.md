# node:crypto

### API information

Reference: `node:crypto`; aliases: `crypto`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/crypto.go. Reading this reference evaluates no target module and makes no provider request.

Offers hashing, HMAC, random values, key derivation, and a limited Web Crypto surface. The global crypto is also installed. These exports are the implemented API, not the full Node crypto module.

### Exports

`constants`, `createHash`, `createHmac`, `default`, `getHashes`, `getRandomValues`, `hkdf`, `hkdfSync`, `pbkdf2`, `pbkdf2Sync`, `randomBytes`, `randomFillSync`, `randomInt`, `randomUUID`, `subtle`, `timingSafeEqual`, `webcrypto`

The Web Crypto subtle surface is intentionally small and source-defined; do not assume unsupported algorithms or key formats.

The contract corrections below are in unreleased source after v2.19.0.
Installed v2.19.0 executables retain the earlier permissive key behavior.

## webcrypto

`webcrypto` is the same object as global `crypto`. The named `subtle` export
and `webcrypto.subtle` are global `crypto.subtle`. These computations require
no filesystem, network, or command grant and are available in Workers.

## subtle

Supported operations are `digest`, raw `importKey` / `exportKey`, HMAC `sign` /
`verify`, and PBKDF2/HKDF `deriveBits` / `deriveKey` to HMAC. Digest and HMAC
hashes are SHA-1, SHA-256, SHA-384, and SHA-512. Web names normalize ASCII case;
Node aliases such as SHA256 and MD5 do not extend this registry.

Subtle methods return promises and snapshot BufferSource bytes and algorithm
parameters before asynchronous calculation. DataView and typed-array offsets
are respected. Ordinary arrays/strings, detached buffers, and shared-backed
BufferSources are rejected. CPU work uses Go cryptographic primitives off the
VM owner when a Loop exists; execution/Worker cleanup cancels and joins it.

## subtle.importKey

`importKey("raw", data, algorithm, extractable, keyUsages)` returns a branded
CryptoKey. HMAC needs a hash and accepts sign/verify usages. PBKDF2/HKDF keys
must be non-extractable and accept only deriveKey/deriveBits. Secret keys need
at least one usage. Invalid enum values and invalid algorithm data use their
specified TypeError/DOMException rejection rather than permissive coercion.

## subtle.exportKey

`exportKey("raw", key)` copies an extractable HMAC key. It reads private
extractability; changing visible metadata or shadowing an attribute cannot
grant export permission. A non-extractable HMAC key rejects InvalidAccessError.
PBKDF2/HKDF have no export operation, so export rejects NotSupportedError before
testing extractability. Other formats, including JWK, remain unsupported.

## subtle.sign

`sign("HMAC", key, data)` requires HMAC sign usage and returns an ArrayBuffer.
The algorithm/hash comes from private imported state. Readable metadata does
not authorize changing the key's algorithm or usages.

## subtle.verify

`verify("HMAC", key, signature, data)` requires verify usage independently of
sign and returns a boolean. Modified or differently sized signatures return
false; verification does not require granting signing authority.

## subtle.deriveBits

PBKDF2 requires hash, salt, and a positive unsigned iteration count; HKDF
requires hash, salt, and info. The supported paths require explicit byte-aligned
bit lengths. Current draft behavior permits zero-bit PBKDF2 output after
validating iterations. HKDF retains its algorithm-defined 255-hash-block bound.

## subtle.deriveKey

`deriveKey(parameters, baseKey, hmacParameters, extractable, usages)` checks
deriveKey usage without requiring deriveBits. Its HMAC target requires a hash;
the default target length is the hash block size, 512 or 1024 bits. It applies
the target import rules and requested extractability/usages to the new key.

### Remaining Web Crypto gaps

Key generation, encrypt/decrypt, wrapKey/unwrapKey, JWK/SPKI/PKCS8 formats,
public-key algorithms, complete Crypto/SubtleCrypto IDL shape, and CryptoKey
structured cloning remain outside this slice. Unsupported key cloning raises
DataCloneError instead of producing an ordinary metadata object. Workers can
import their own raw material. The jose 6.2.15 HMAC fixtures validate JWT/JWS
HS256/384/512 paths; they do not imply complete jose compatibility.


### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
