# noebot migration to NOE v2.14.0

Update noebot for NOE v2.14.0, which moves Promise/Web Streams process spawning
to `node:child_process` and removes `noe:process` without an adapter. Raise
noebot's minimum NOE version to **2.14.0** and deploy the matching executable
and application sources together. The required migration is small; filesystem
adoption opportunities are separate work items below.

NOE release: [v2.14.0](https://forge.harakara.site/littleisland/noe/releases/tag/v2.14.0).
Repository: [littleisland/noebot](https://forge.harakara.site/littleisland/noebot).
The inspected remote `main` was `45de74771d9c048448afb1e7f1039b0b45a01443`,
rechecked on 2026-11-06. Start from current remote sources and read noebot's
own instructions; an older local checkout may not contain the inspected code.

## Required source changes

1. In `app/main.mjs` and `tests/s1.mjs`, replace:

   ```js
   import {spawn} from 'noe:process';
   ```

   with:

   ```js
   import {spawn} from 'node:child_process';
   ```

   Continue awaiting `spawn` and using the existing stream options, `exited`,
   `kill()`, and `close()`. NOE's `spawn` still returns a Promise and uses Web
   Streams; it does not introduce Node.js ChildProcess events or Node Streams.

2. Update `validateStartup()` in `app/main.mjs`: change the v2 minor-version
   lower bound from 11 to 14 and the error to `NOE >= 2.14.0 is required`.
   Preserve the existing major-version handling. Update README requirements,
   current operations/type-checking guidance, and relevant fixtures that
   advertise the old minimum. Preserve historical verification records.

3. Search current application and test sources for additional uses of
   `noe:process`, `noe:io`, synchronous stdio methods, and the old pipe helper.
   The inspected revision only required the two spawn-import edits. If newer
   sources use old stdio, migrate to `node:process` byte Web Streams and
   `source.pipeTo(destination, options)`; explain any newly affected callers.

NOE v2.13.0 cannot run the new spawn import. Keep the executable and application
version requirements aligned for both the launcher and children started through
PATH. Retaining support for the removed module is outside this migration.

## Verification

Put the published v2.14.0 executable on an isolated test PATH before testing.
Invoking an absolute candidate alone is insufficient because the launcher
and tests also start child processes with the command `noe`.

From the noebot repository root, using that PATH:

```sh
noe --version
noe run --no-inst tests/s1.mjs
noe run --no-inst tests/s2.mjs
noe run --no-inst tests/s3.mjs
noe run --no-inst tests/images.mjs
noe run --no-inst tests/telegram_commands.mjs
noe run --no-inst tests/telegram_typing.mjs
noe run --no-inst tests/web_timing.mjs
```

The expected version is `noe v2.14.0`. These suites previously passed on the
verified feature candidate with only the two import edits: S1 31, S2 83,
S3 45, images 11, Telegram commands 20, Telegram typing 13, and Web timing 5,
for 208 checks. Counts may change with newer noebot sources. The raised
minimum-version check must also accept 2.14.0 and reject older v2 versions;
cover that behavior in the existing startup tests. Type-check against the
declarations from the selected NOE release using noebot's documented setup.

These checks use local fixtures. Real providers, Telegram, and production
conversations are not needed for this API migration. Run additional checks
only when the current diff affects behavior outside these suites.

Implementation evidence is retained on the NOE workspace under
`/tmp/noe-cancellation-cleanup-v90s154r/results.json`; earlier process and
stdio evidence is in `/tmp/noe-process-integration-863auo4a/` and
`/tmp/noe-stdio-integration-dkaizbjk/`. The checked-in
[NOE release record](../releases/release-checkpoints.md) is the durable summary.

## Deployment ordering

Prepare and verify the noebot patch before replacing its shared NOE executable.
The old noebot source fails to import the removed module under v2.14.0; the
new source cannot use the new spawn entry point under v2.13.0.

For the noebot deployment task, stage the release binary and matching source,
verify its published SHA256SUMS, and coordinate the switch with the normal
service stop/start procedure. Keep the prior executable and source revision
as a pair for rollback. Confirm that the launcher's PATH and its children's
PATH select v2.14.0, then check startup, local health/status, and logs using
noebot's operations guide. Existing processes retain their loaded executable
until restarted.

This NOE release publishes the assets and these instructions. It does not
change the actual noebot repositories, shared installed NOE executables, or
running services. Perform deployment in the noebot-side task when requested.

## Optional filesystem adoption

After the required migration, consider these changes in order. They have no
measured application performance result yet. Explain compatibility changes
for each work item before implementation, as requested by the user.

1. **Notes directory traversal.** In `app/notes.mjs`, use
   `readdirSync(dir, {withFileTypes: true})` in `Notes.list()` to remove the
   separate per-entry `lstatSync` call. Preserve symlink rejection, note-name
   validation, temporary-directory exclusion, sorted results, and the path
   checks used for reads and writes.

2. **Missing-file fallback cleanup.** `app/store.mjs` already takes the
   `ENOENT` fast path with the new NOE. With the higher minimum, remove the
   older parent-listing fallback in `lstatMaybe()` and update its comment if
   useful. This is cleanup rather than a new performance gain.

3. **Asynchronous durable persistence.** FileHandle `sync()` can remove the
   external GNU `sync -f` processes and narrow synchronization from an entire
   filesystem to the open descriptor. Plan this separately: Store transactions
   and `syncNow()` currently reject asynchronous/thenable callbacks or adapters.
   Address initialization, write serialization, caller completion, and handle
   cleanup across Store, Notes, and image storage together.

   Preserve private permissions, complete and sync the temporary file before
   rename, and sync the destination parent directory before durable success.
   A file-only sync cannot replace the post-rename directory sync. Verify
   directory-handle syncing on the Linux target. Preserve the distinction
   between failure before publication and an uncertain commit after rename,
   including poisoning that stops subsequent writes. Remove the GNU sync
   prerequisite only after every persistence caller has migrated.

## Optional cancellation and cleanup adoption

`AbortSignal.any()` and `await using` do not require noebot rewrites. Preserve
the transport behavior in `app/telegram.mjs`: distinguish caller stop from
deadline expiry, include response-body work in the deadline, and clear timers
and listeners after completion. Combining signals alone does not replace
that adapter. The host launcher's explicit child cleanup remains valid;
`await using` is an optional way to express scoped ownership.

Return the required migration diff, validation results, selected NOE version,
and deployment status. Keep optional filesystem and transport redesigns
separate from the compatibility migration unless explicitly included in the
noebot-side request.
