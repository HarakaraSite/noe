# Web Crypto contract correction

Included in v2.20.0: correct existing Web Crypto contracts, inventory the
remaining standard API gaps, and validate an unpatched real SDK. Publication
and installation are recorded in the [release checkpoints](../releases/release-checkpoints.md).
Noebot, real login, and application-provider calls remain outside this work.
The [API policy](stdlib-api-policy.md) remains current.

The HMAC generation/JWK follow-up below is committed as `f4a1418` for v2.30.0.
The original v2.20.0 correction/acceptance records remain historical; current
supported contracts are in the [API reference](../../internal/apidocs/content/stdlib-crypto.md#subtlegeneratekey).

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
  ordinary arrays/strings, detached buffers, SharedArrayBuffer-backed inputs,
  and resizable ArrayBuffer-backed inputs where Web IDL disallows them.
  Resizable backing is rejected explicitly as part of the
  [go-quickjs v0.37.1 integration](go-quickjs-v0.37.1-upgrade.md), including
  out-of-bounds DataViews whose length getters now return zero.
  Snapshot conversion and validation
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
| importKey | raw/JWK HMAC; raw PBKDF2/HKDF | SPKI/PKCS#8, non-HMAC JWK, and public/private key types. |
| exportKey | Extractable HMAC raw/JWK material | SPKI/PKCS#8 and non-secret keys. Non-extractable key export is deliberately rejected by contract. |
| sign / verify | HMAC with the SHA hashes above | RSA, ECDSA, Ed25519 and other signature families. |
| deriveBits / deriveKey | PBKDF2/HKDF; derived HMAC keys | ECDH/X25519 and derived AES targets. |
| generateKey | HMAC with SHA-1/256/384/512 from v2.30.0 | Other key/algorithm families remain unsupported. |
| encrypt / decrypt | Not implemented | AES and RSA-OAEP plus their parameter/result contracts. |
| wrapKey / unwrapKey | Not implemented | Wrapping algorithms and dependent key-format support. |
| CryptoKey cloning | Explicit DataCloneError when cloning is unsupported | Standard structured cloning/Worker transport of a CryptoKey with private state preserved. Workers can import their own raw material. |
| Complete IDL interface shape | Eight supported subtle members and branded CryptoKey | Crypto/SubtleCrypto interface constructors, their prototype/receiver model, and encrypt/decrypt/wrapKey/unwrapKey remain incomplete. |

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

At v2.20.0, probes confirmed that jose generateSecret failed on missing
generateKey; CryptoKey exportJWK rejected NotSupportedError, and AES-GCM JWE
rejected an unsupported algorithm. That inventory recorded five missing subtle
methods, missing Crypto/SubtleCrypto constructors, and explicit CryptoKey clone
rejection. The HMAC/JWK follow-up below addresses the first two SDK gaps.

Implementation verification is native Linux amd64 with the existing pinned
engine and Go dependencies. No local six-target build or full network E2E was
repeated. Development candidates remain at
`/var/tmp/noe-webcrypto-derivekey-qgsnbji4/noe` and
`/var/tmp/noe-webcrypto-46m8xyzx/noe` as historical implementation artifacts.

v2.20.0 publication and both vm201 installations are complete. CI builds all
six distribution targets; downloaded assets pass checksum/build metadata
verification. The published native asset passes contract/review/example/docs
checks and actual jose main/Worker/Node interoperability. Direct published
Go consumers also pass. Both executable paths contain the verified asset,
with previous v2.19.0 binaries retained. See the
[publication and installation checkpoint](../releases/release-checkpoints.md#v2200-publication-and-installation--2026-11-08)
and `/var/tmp/noe-v2.20.0-release-el4vn8lo` for final evidence.

## HMAC generation and JWK — 2026-11-18

The user selected the previously memoed HMAC generateKey and JWK expansion.
This adds the standard HMAC path only, reusing private CryptoKey policy and
the configured host entropy source. Web Crypto's HMAC
[operation procedures](https://w3c.github.io/webcrypto/#hmac-operations),
[JWK usage metadata](https://www.rfc-editor.org/rfc/rfc7517.html#section-4.3),
and [octet key encoding](https://www.rfc-editor.org/rfc/rfc7518.html#section-6.4)
are the contract references. txiki.js supplied the earlier feature comparison.

Generation supports default hash block lengths and explicit positive uint32
bit lengths, including partial final bytes. It uses the existing borrowed
Random reader on the VM owner, without applying getRandomValues' 64 KiB
quota. Read failure becomes OperationError; no cryptographic family or host
grant is added. Node-style crypto helpers retain their current contracts.

JWK dictionaries are converted and snapshotted before algorithm getters.
Imports validate octet material, hash/alg agreement, use, key_ops duplicates
and requested-usage inclusion, and ext. Additional distinct key_ops entries
are tolerated and do not expand requested authority. Exports return independent
dictionaries/usage arrays from private key state. Raw imports/exports and KDFs
remain available. Non-extractable keys cannot be exported in either format.

The decoder requires JWA's unpadded base64url alphabet. The checked Node 24.21.0
also accepts padding/whitespace; those permissive cases are recorded as a
comparison difference rather than used to define NOE's contract. Empty material
and invalid encoding reject DataError. Tail bits follow the supported bit length.

Focused main/Worker tests cover generation lengths, entropy selection/failure,
round trips, policy and metadata tampering, dictionary snapshots/conversions,
and invalid JWK data. The pinned unmodified jose 6.2.15 now exercises generation,
JWK export/import and HS256/384/512 signing/verification across Node, NOE main,
and Worker. Bounded review identified a BufferSource/JWK union distinction and
its error ordering. Real buffer/view arguments keep their category through
algorithm normalization, then reject for JWK format; shared/resizable/detached
backing still fails argument conversion. Regression cases cover normal and
JWK-expando buffers, unsupported algorithm precedence and zero-length/usage
conflicts. Final verification evidence: `/var/tmp/noe-hmac-jwk-pfai1v01`.

Verification is complete locally. The final unmodified SDK report checks all
four Node/main/Worker directions, with nine generated-key peer tokens per
direction and HS256/384/512 generation/import/export paths. AES-GCM JWE stays
unsupported; key cloning still rejects DataCloneError. The checker asserts the
eight supported subtle members and remaining interface gaps.

All other packages passed the whole-tree unit/E2E run. Its sole failure was a
new public-consumer fixture that exported sign-only key_ops then requested
verify on re-import; the fixture now generates sign/verify authority. The full
stdlib package and both checkout/artifact consumers were rerun successfully.
Production sources did not change after the broad run. Crypto-focused race,
vet, type fixtures, lint, and native embedded-reference checks pass. Previous
filesystem race evidence is reused after matching its unchanged runtime paths.

AES/RSA/EC/Ed25519, key wrapping, key cloning and full Crypto/SubtleCrypto
interface objects remain separate gaps. Byte streams/BYOB remain memo-only.
Dependencies and public Go signatures are unchanged. The implementation is
committed as `f4a1418` and included in v2.30.0 release preparation.
