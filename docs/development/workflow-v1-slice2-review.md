> Historical record of the paused machine-gated implementation. Its technical contracts are superseded by [the agent-first redesign](workflow-agent-first-design.md) and [current implementation record](workflow-agent-first-implementation.md); do not use it to launch or constrain the current workflow.

# Workflow v1 slice 2 review

Reviewed the current requirements/plan, changes since slice-1 commit `62072b5`, and new `execution.js`, `git.js`, and `test-execution.js`. CLI/example/finished launch docs remain slice 3 and are not findings. No implementation modifications or delegation.

## S2-N1 — New unowned source bypasses ownership checks and can be omitted from delivery (high)

**Evidence:** `git.js:67` applies ownership validation only to tracked changes. New visible files are returned in `change.added` but not rejected when outside ownership. Evidence/signatures and commit selection then filter those files away (`git.js:86,90,97`); final cleanliness checks also ignore unowned additions (`git.js:123,128`). `execution.js:64,81-85` validates worker-claimed paths without checking all actual additions.

**Focused real-Git reproduction:** With ownership `["src/main.sh"]`, create:

```sh
# src/main.sh
cat src/helper.txt
```

and a new visible `src/helper.txt` containing `ok`. The guard's assertion accepts both additions; the actual local command returns `ok`. Commit, finish and push all succeed, but only `src/main.sh` is committed. Running the command in a fresh checkout of the pushed branch fails with `cat: src/helper.txt: No such file or directory`.

**Impact:** An ordinary omitted owned-path/amendment can leave required source outside the reviewed diff and delivered commit while local verification passes. The workflow can approve/push an incomplete product. This concerns the advertised ownership/review/delivery path (R4/R5/R10), not an OS-sandbox guarantee.

**Fix:** Check actual new visible source after worker tasks against approved ownership instead of relying on `changedPaths`. Reject unapproved additions before checks/review/commit, or explicitly classify legitimate non-product command outputs rather than silently treating every outside addition as unrelated. Keep deliberate ignored-diagnostic exclusions. Verify that an unowned new dependency cannot reach push and that a correctly approved amendment includes it in review and commit.

## S2-N2 — Valid custom fetch mappings cause a false failure after successful push (medium)

**Evidence:** `git.js:146-150` assumes Git may update only `refs/remotes/<remote>/<pushBranch>`. Git uses the configured fetch refspec to select its remote-tracking ref, which need not have that conventional name.

**Focused real-Git reproduction:** Configure the local remote before preflight:

```sh
git config remote.origin.fetch '+refs/heads/*:refs/remotes/cache/*'
```

Pushing configured `topic:delivered` to a local bare remote succeeds and updates `refs/remotes/cache/delivered`. The controller then throws `git-mutation: Push hook changed unverified Git state`, despite no hook or unauthorized mutation. The remote contains the expected delivered HEAD and state correctly says `pushed:true`.

**Impact:** A valid repository configuration passes preflight and delivers the branch, but the workflow reports failure rather than completion. Users must diagnose a false integrity error after the sole final push (R1/R6/R11).

**Fix:** Derive permitted tracking-ref updates from the configured fetch mappings for the actual pushed destination, and verify their expected object IDs, rather than hardcoding conventional tracking names. Alternatively deliberately avoid tracking-ref updates through the chosen push contract. Keep rejection of genuinely unrelated ref mutations.

## Validation and remaining scope

Both findings were demonstrated with isolated temporary real Git repositories and local bare remotes; all fixtures were removed. No network/provider calls or full suites were run. Existing tests were inspected for repair/re-review, actual model switching, integration correction commits, protected inputs, literal staging, hook mutation, ignored-file handling, and rejected-push retention. No other concrete findings from this focused pass.
