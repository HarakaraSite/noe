# Web Crypto contract correction

Implemented and verified locally after v2.19.0: correct existing Web Crypto
contracts, inventory the remaining standard API gaps, and validate an unpatched
real SDK. Publication, installation, noebot, real login, and paid providers are
outside this request. The [API policy](stdlib-api-policy.md) remains current.

## Scope and references

Keep SHA-1/256/384/512 digest, HMAC raw import/export/sign/verify, and raw
PBKDF2/HKDF import/deriveBits plus deriveKey to HMAC. Preserve the Node-style
crypto helpers and identity of their `webcrypto` / `subtle` exports with the
globals. Do not add AES, RSA, EC, JWK, or key generation merely to fill a list.

Use the [W3C Recommendation](https://www.w3.org/TR/2017/REC-WebCryptoAPI-20170126/)
for the established algorithms and consult the
[current editor's draft](https://w3c.github.io/webcrypto/) (22 October 2026
snapshot checked on 7 November) for clarified
bindings. Record any intentional remaining limitation instead of claiming full
Web Crypto support. Algorithm availability remains a product choice; behavior
of the supported operations must follow their specified contracts.

The current draft clarifications used here include normalized key-usage
ordering, HMAC's default derived length in bits, and zero-bit PBKDF2 output
after iteration validation. For export, unsupported algorithm operations are
checked before extractability (both references specify this order); a
PBKDF2/HKDF export therefore differs from a non-extractable HMAC export. A
secondary runtime's different rejection is recorded rather than used to
override those procedures.

## Existing contracts to correct

- CryptoKey is an illegal constructor with branded readonly attributes.
  Internal material, algorithm, usages, and extractability are held separately
  from caller-visible objects. Input arrays and dictionaries are snapshotted.
  Metadata getters cache their objects; mutating or shadowing those visible
  objects must not change the internal authority or algorithm. Do not freeze
  arbitrary caller inputs or add host permissions.
- Enforce the selected algorithm and operation's key usage independently.
  Verify-only keys must verify without needing sign permission. Extraction
  uses private state. Distinguish bad input/branding, unsupported operations,
  algorithm/data validation, and invalid access using the specified errors.
- Copy BufferSource bytes with their exact view offset and length; reject
  ordinary arrays/strings, detached buffers, and SharedArrayBuffer-backed
  inputs where Web IDL disallows them. Snapshot conversion and validation
  before yielding. Preserve rejected-Promise behavior for subtle methods.
- Normalize only registered Web algorithm names (ASCII case-insensitive),
  rather than accepting Node hash aliases such as MD5 or SHA256. Formats and
  key usages use their Web IDL enum spellings. Validate required dictionaries,
  usage lists, HMAC lengths, PBKDF2 iterations, and KDF length constraints.
  PBKDF2/HKDF raw keys remain non-extractable, with derivation-only usages.
- `deriveKey` checks deriveKey permission and performs its internal derivation
  without additionally requiring deriveBits permission. HMAC's default length
  is the selected hash's block size. Apply the supported algorithm's import
  rules to the resulting bytes and preserve extractability/usages. Check the
  base-key algorithm and usage before performing the target get-key-length
  operation, as specified by deriveKey steps 12-14.
- getRandomValues accepts the standard integer typed arrays, including BigInt
  views; its 65,536-byte limit and invalid-view errors are DOMExceptions with
  the standard names. Keep randomUUID's existing v4 behavior. No arbitrary new
  crypto quota is introduced.
- SHA/HMAC/KDF calculations use existing Go cryptographic primitives. With a
  Loop, native calculations run off the VM owner and completion returns to that
  owner. Execution/Worker close cancels and joins pending work. KDF cancellation
  must not leave an unbounded CPU goroutine running after close. Direct legacy
  WebAPIs installation without a Loop may return settled computation promises.

CryptoKey structured cloning and the Crypto/SubtleCrypto interface's full
shape must be assessed explicitly in the gap inventory. If cloning is not
implemented in this slice, reject it clearly instead of degrading a key into
an ordinary object containing misleading metadata. Record that limitation.

## Integration and acceptance

Public profiles and Workers retain independent grants and the same selected
Random source; no FS/network/command grant is needed by cryptography. Keep
Node crypto helpers' existing byte/text conventions separate from stricter
Web BufferSource conversion. Update ambient types, embedded references,
examples, public Go consumer, and the session handoff together.

Use regression cases for verify-only signing, extractability and metadata
tampering, invalid usages and keys, byte-view snapshots, KDF vectors/constraints,
and lifecycle behavior. Compare representative cases against the standard and
another Web Crypto implementation; do not make existing permissive tests the
reference. Correct test expectations that contradict the external contract.

SDK target: unpatched `jose` 6.2.15, pinned npm integrity
`sha512-qdF6ewcZARtef63fJ9AKKZPgxw+t7USH6HLXYcD4CrdmSvDp79rpJSPFEKc90QBeGS63u1KQtN2MZQHC0yDIzA==`.
Exercise real JWT/JWS HS256/384/512 signing and verification using raw secrets
and supported CryptoKeys, rejection of modified signatures, expiry/issuer /
audience validation, Worker execution, and independent interoperability.
Preparing SDK dependencies may use Node/npm; NOE must execute the test. Record
unsupported SDK paths such as public-key algorithms and JWE without presenting
the successful HMAC path as complete SDK compatibility.

## Standard API inventory

The inventory distinguishes an implemented interface member from the
algorithms and formats it can actually use. It is a scoped compatibility
record, not a claim that the full Web Crypto specification is implemented.

| Area | Supported slice | Remaining gap |
| --- | --- | --- |
| Randomness | Integer-array getRandomValues and randomUUID v4 | No hardware-key or browser secure-context model is added by this host runtime. |
| digest | SHA-1, SHA-256, SHA-384, SHA-512 | Other algorithms remain unsupported. Node hash aliases do not extend the Web registry. |
| importKey | raw HMAC, PBKDF2, HKDF | Native JWK, SPKI, PKCS#8 imports and public/private key types. |
| exportKey | Extractable HMAC raw material | JWK/SPKI/PKCS#8 and non-secret keys. Non-extractable key export is deliberately rejected by contract. |
| sign / verify | HMAC with the SHA hashes above | RSA, ECDSA, Ed25519 and other signature families. |
| deriveBits / deriveKey | PBKDF2/HKDF; derived HMAC keys | ECDH/X25519 and derived AES targets. |
| generateKey | Not implemented in this slice | Even HMAC generation remains absent; jose generateSecret therefore has a known gap. |
| encrypt / decrypt | Not implemented | AES and RSA-OAEP plus their parameter/result contracts. |
| wrapKey / unwrapKey | Not implemented | Wrapping algorithms and dependent key-format support. |
| CryptoKey cloning | Explicit DataCloneError when cloning is unsupported | Standard structured cloning/Worker transport of a CryptoKey with private state preserved. Workers can import their own raw material. |
| Complete IDL interface shape | Seven supported subtle members and branded CryptoKey | Crypto/SubtleCrypto interface constructors, their prototype/receiver model, and five missing subtle methods remain incomplete. |

Public-key support and encryption are future implementation choices. Adding
them requires their operation contracts, formats, key policies, and tests;
creating method names alone would not make those use cases work. No new
algorithm or method stub is added solely to change feature-detection output.

## Verification record

Evidence directory: `/var/tmp/noe-webcrypto-46m8xyzx`.

The unpatched jose HMAC happy path already passes installed v2.19.0. It does
not detect the existing usage/extractability violations, so this slice starts
with direct contract regressions instead of treating SDK success as proof of
standard conformance. The Node execution of the same fixture also passes.
The corrected source passes affected unit packages and focused public/native
race tests. Tests cover vectors, private key authority, readonly/cached
metadata, byte offsets/snapshots/detachment/shared rejection, standard errors,
KDF constraints, Worker execution, and native cancellation/drop ownership.
The current zero-output PBKDF2 procedure is tested separately from zero
iterations. TypeScript positive/negative fixtures, embedded reference catalog,
lint and affected vet pass. External Go consumers build and run from both the
checkout and a standalone source-module artifact without new dependencies.

The original verify-only-sign and extractability regressions fail on the
v2.19-era source. A further parent source review found metadata accesses that
could invoke prototype hooks on private records/arrays. Null-prototype state,
own cache fields, and own data-property creation remove those callbacks. A
functional test that installs throwing prototype hooks fails on the previous
facade and passes after correction without inspecting private key material.

The independent reviewer stopped during its initial pass because its automatic
screening reported possible cybersecurity risk. It did not complete a review
or re-review. The parent completed source/contract checks and the regressions;
the result must not be described as an independently reviewed full API audit.

A subsequent user-requested independent source review completed with pi,
`opencode-go/glm-5.3`, thinking `max`, against commit `6c8a23d`. The report and
parent dispositions are in `/var/tmp/noe-webcrypto-pi-review-dodw0o1o`.
Primary-specification checks and small runtime probes rejected the report's
HMAC import-length and fractional EnforceRange claims. One low-severity
deriveKey error-precedence finding was accepted: a base key without deriveKey
authority and an HMAC target length of zero must reject InvalidAccessError
before the target get-key-length operation can reject TypeError.

The follow-up moves that operation after the base-key algorithm/usage check.
Its public regression fails before the fix and passes afterwards, covering
both missing usage and a mismatched base-key algorithm; a permitted base key
still rejects TypeError for zero target length. Focused public/native Web
Crypto and legacy subtle tests, lint, a native build, and the nine-case CLI
review fixture pass. Eight fixture results remain unchanged; only the
accepted finding's error changes. Evidence and the current development
candidate are in `/var/tmp/noe-webcrypto-derivekey-qgsnbji4`. The parent
verified this narrow fix; no independent fix re-review is claimed. Earlier
race/type/consumer/SDK results are reused for their unchanged paths.

`scripts/check-webcrypto-sdk.py` validates the unmodified jose 6.2.15 Web API
ESM distribution. Its final report is
`/var/tmp/noe-webcrypto-46m8xyzx/sdk-validation-final/summary.json`: 46 SDK files
match their before/after fingerprints; main and Worker each pass 27 fixture
checks, including Node-produced tokens. Node also verifies NOE-produced tokens,
and deterministic signatures match in both directions. The fixture includes
HS256/384/512 JWT/JWS with raw and CryptoKey inputs, modified-signature rejection,
expiration, issuer and audience validation. No SDK patches, polyfills, network
service, login, or paid provider are used during execution.

Actual probes confirm that jose generateSecret currently fails on missing
generateKey, CryptoKey exportJWK rejects NotSupportedError, and AES-GCM JWE
rejects an unsupported algorithm. A runtime inventory also confirms the five
missing subtle methods, missing Crypto/SubtleCrypto constructors, and explicit
CryptoKey clone rejection. These are remaining capabilities, not completed
work. A future small expansion can use these confirmed SDK requirements.

Verification is native Linux amd64 with the existing pinned engine and Go
dependencies. No new six-target build, full network E2E, release, push, or
installation is claimed. Installed binaries remain v2.19.0; the development
candidate after the error-precedence fix is
`/var/tmp/noe-webcrypto-derivekey-qgsnbji4/noe`; the earlier candidate remains
at `/var/tmp/noe-webcrypto-46m8xyzx/noe`.
