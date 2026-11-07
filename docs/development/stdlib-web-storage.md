# Web Storage

Implemented and verified at `dfa497e` on 2026-11-07; included in v2.19.0.
The [API policy](stdlib-api-policy.md) remains authoritative. Publication and
local installation are recorded in the [release checkpoints](../releases/release-checkpoints.md).

## JavaScript contract

`Storage` and `sessionStorage` accompany the default Web APIs. `Storage` is
an illegal constructor. `sessionStorage` is one stable, initially empty object
per runtime, including each Worker; separate executions and Workers do not
share session data.

`localStorage` is installed only when the host explicitly selects
`Modules.LocalStorage = &stdlib.LocalStorageOptions{Path: "state.json"}`.
Selection requires `Modules.FS`; the selected filesystem's Root, Cwd, and
ReadOnly policy applies. CLI `run` and `eval` accept `--local-storage path`.
No default path, directory creation, environment-variable discovery, or other
implicit persistent grant is added. The containing directory must exist.
Relative paths are fixed against the execution Cwd before starting Workers.

Both objects implement synchronous `length`, `key(index)`, `getItem(key)`,
`setItem(key, value)`, `removeItem(key)`, and `clear()`. Missing entries return
null through methods and undefined through named property reads. Keys/values
use Web IDL DOMString conversion, including rejection of Symbols; required
arguments and receiver brands are checked. `key` converts its index to an
unsigned 32-bit integer. Named assignment/deletion, enumeration, descriptors,
and method-name collisions follow Storage's legacy named-property behavior.
UTF-16 strings, including unpaired surrogates, round-trip unchanged.
Storage objects cannot be structured-cloned or transferred.
The Proxy facade rejects `Object.defineProperty` requests with
`configurable: false` before changing storage: ECMAScript Proxy invariants
cannot represent a virtual nonconfigurable named property. Configurable data
descriptors are supported, including a value of undefined (stored as the
string `"undefined"`).

The references are getter-only globals. Objects have `Storage.prototype` and
the Storage toStringTag. Insertion order is used for key enumeration; updating
an existing key retains its position. Browser storage has implementation-defined
iteration order, so applications should not rely on ordering across engines.
No arbitrary storage quota is introduced.

## Persistence and ownership

Local operations read the latest committed file, rather than using a stale
per-runtime cache. A mutation commits atomically through the existing
filesystem backend before returning. Failed writes leave previous contents
intact. Missing files represent empty storage and are created on the first
mutation. Invalid existing data raises InvalidStateError and is not reset.
ReadOnly rejects mutation with SecurityError; native I/O errors retain useful
filesystem codes. Storage must not silently fall back to session memory.

Use a versioned JSON document containing ordered key/value pairs. Parse and
stringify the strings in JavaScript so Go's Unicode conversion cannot replace
unpaired surrogates. Serialize read/modify/commit for the same canonical file
within this process, including main runtimes and Workers. Do not retain open
file handles or background jobs. Independent OS processes have no transaction
coordination; use `noe:sqlite` for cross-process transactional state. Ordinary
FS edits of the file are likewise outside storage-operation coordination.

These globals are a host adaptation of the
[WHATWG Storage interface](https://html.spec.whatwg.org/multipage/webstorage.html).
NOE has no Window, Document, browser origin partitioning, or storage events.
Worker exposure is an explicit NOE extension; matching localStorage paths share
data and sessionStorage remains runtime-local. No grant for network, commands,
or other modules follows from selecting localStorage.

## Verification

Evidence: `/var/tmp/noe-web-storage-bvqxqjs1`.

- Affected unit packages pass: internal stdlib, jsexec, noestdlib, noeprofile,
  runtime, CLI, and the public stdlib package. Public tests cover DOMString and
  property behavior, required arguments/brands, method-name collisions,
  structured-clone rejection, UTF-16 preservation, persistence, Root/Cwd,
  ReadOnly, invalid-data preservation, missing-directory failures, and cleanup.
- Main/Worker tests verify independent session data, shared committed local
  changes, concurrent independent-key updates, and immutable option snapshots.
  Native tests verify canonical symlink lock keys and contended cancellation.
- CLI E2E tests pass for separate-process persistence, Worker access, and the
  real example's increasing counter. TypeScript positive/negative fixtures,
  embedded API documentation, affected vet, and JavaScript lint pass.
- The checkout and source-module-artifact external Go consumers both build and
  run. No dependency or engine change is introduced. The final consumer's
  runtime source is compared against the local manifest; actual execution is
  on Linux amd64. Existing unchanged dependency/platform evidence is reused;
  no new six-target build or full network E2E/race suite is claimed here.
- Focused final race tests cover native/public storage, profile setup, and CLI
  integration. An independent bounded review found one reentrant prototype
  descriptor getter that could deadlock the native file lock. Runtime property
  descriptors now have null prototypes. The current regression fails against
  the earlier module artifact and passes against the corrected source,
  including inherited toJSON/get hooks and UTF-16 persistence. One fix-only
  re-review confirms resolution with no further actionable finding.

v2.19.0 is published and installed at both authorized vm201 executable paths.
Release evidence is `/var/tmp/noe-v2.19.0-release-pmyv5yje`; previous binaries
and installed checks are at `/var/tmp/noe-v2.19.0-install-4yu_9iy0`.
Go consumers using unkeyed `Modules` literals must use keyed fields because
`LocalStorage` adds a public struct field. Existing JS APIs are not removed.
