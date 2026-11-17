# Agent library optimization: context serialization

Local implementation on top of v2.12.0, 2026-11-05. The baseline is
`159f6c0`. This work changes private context calculations in `internal/agent`.

## Observed cost and change

Context preparation measures message/tool JSON, calculates protected and
removable-block weights, and hashes the prepared request. Request admission
checks the digest again. Previously, each calculation materialized complete
JSON byte slices even when only a length or SHA256 digest was needed. These
calculations also run within the existing Host state lock.

Ordinary histories now use native `encoding/json/v2.MarshalWrite` with a byte
counter or SHA256 writer. Size, hash, and block-weight calculations retain
the same JSON representation, validation, token rounding, and image-token
accounting. Both Go Client and JavaScript agent execution use this shared
implementation. Public APIs and ownership/snapshot rules are unchanged.

Go 1.27's streaming encoder discards its buffer when capacity exceeds
64 KiB. An initial all-streaming version regressed with individual 512 KiB
messages because their encoded buffers were allocated repeatedly. The final
implementation uses the existing buffered representation when a raw message
or tool exceeds 64 KiB. This is a measured allocation heuristic, not an input
limit. Invalid history is still rejected during measurement; the digest
helper also retains its previous behavior for partial output on errors.

Implementation: [JSON helpers](../../internal/agent/json.go),
[measurement](../../internal/agent/context.go), and
[history weights/digests](../../internal/agent/context_history.go).

## Measurements

Go 1.27.2, Linux amd64, CGO disabled, Intel Core i9-13900HX. Each value below
is the median of five isolated 250 ms benchmark runs. Inputs contain one
small tool definition, two protected messages, and subsequent removable
assistant blocks. Content sizes exclude the surrounding JSON metadata.

The measured operation performs history measurement, block-weight calculation,
and digest calculation. Allocated bytes are cumulative per operation; they
are not peak memory or RSS. This excludes the rest of context preparation,
runtime startup, hooks, and provider latency.

| Message content | Allocated bytes before | Allocated bytes after | Time before | Time after |
| --- | ---: | ---: | ---: | ---: |
| 4 x 4 KiB | 58,508 | 1,972 | 58.1 us | 52.0 us |
| 256 x 4 KiB | 9,902,942 | 123,924 | 5.69 ms | 3.27 ms |
| 2 x 512 KiB | 3,832,370 | 3,760,325 | 3.53 ms | 3.64 ms |

The many-message 1 MiB case reduced allocated bytes by 98.75% and time by
42.49%. The large-message case retains buffering and showed approximately
the baseline allocation and a 3.1% time increase in this run. These short
microbenchmarks do not establish application latency or throughput.

The [benchmark](../../internal/agent/context_benchmark_test.go) remains
available for later comparisons:

```sh
CGO_ENABLED=0 go test ./internal/agent -run '^$' \
  -bench '^BenchmarkContextHistory$' -benchmem -benchtime=250ms -count=5
```

## Verification

[Serialization regressions](../../internal/agent/context_serialization_test.go)
compare measurements and digests to native v2 output for nil/empty collections,
whitespace, Unicode/escapes, large values, many-message histories, and large
tools. They verify rejection of malformed JSON, duplicate names, and invalid
UTF-8, including errors after a streamed prefix. Protected entries, complete
tool blocks, image estimates, comma accounting, and rounding retain the
same block weights.

The following checks passed:

```sh
CGO_ENABLED=0 go test ./internal/agent ./agent ./internal/runtime -count=1 -timeout=4m
CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/agent ./agent -count=1 -timeout=4m
CGO_ENABLED=0 go test -tags=e2e ./tests \
  -run '^(TestContextTrimming.*|TestContextObservationRunAndEval)$' -count=1 -timeout=4m
go vet ./internal/agent ./agent ./internal/runtime
git diff --check
```

These include the external public Go consumer and existing queue/conversation,
steer, trim, image, cancellation, and saved CLI-observation regressions.
Provider wire adapters are unchanged; verification uses local fixtures.

Raw baseline/streaming/final benchmark logs, summaries, verification logs,
and changed-source hashes are retained at
`/tmp/noe-agentlib-opt-g7j4w93x/results.json`. Implementation and validation
are complete; release and executable placement remain separate work.

Subsequent work is recorded in
[conversation appends and incremental SSE assembly](agentlib-checkpoint-sse-optimization.md).

## Revision-scoped preparation reuse — 2026-11-17

Implemented locally on top of `3334012`. Managed context preparation now caches
the measurement totals, protected/block weights and existing JSON digest for
one authoritative conversation snapshot. Repeated preparation of that revision
reuses those calculations, and matching managed model admission reuses its
digest. This follows the existing immutable append protocol; it does not add a
second transcript or change JSON representation, token rounding or image costs.

The key includes snapshot identity and revision, owned exact tool bytes (with
nil/empty collections distinguished), and copied image estimates. Appends,
replacement/restore snapshots, tool changes and image-estimate changes miss.
Explicit-message preparation retains its full validation and digest path.
Successful explicit preparation, including trim acknowledgement, releases an
obsolete cache. Run finish and task-outcome settlement release cache references
so retained results do not pin an earlier transcript. Failed preparation does
not install a newly computed entry.

Lifecycle, image, notification, steering and trim gates remain in their original
order. Model callbacks still receive request-owned bytes. Public APIs, provider
payloads and dependencies are unchanged.

`BenchmarkManagedContextPrepare` exercises the actual Host preparation operation
with a warmed, fixed managed history and automatic trimming disabled. Before and
after use the same benchmark scaffold; the baseline uses a Go overlay restoring
the pre-change preparation/admission sources. The values below are medians of
five 1-second runs with `-cpu=1` on Go 1.27.2, Linux amd64,
Intel Core i9-13900HX.

The benchmark commands inherited CGO/GOFLAGS settings; their historical values
were not recorded. Both sides used the same inherited environment.

| History | Time before | Time after | Allocated bytes before | Allocated bytes after |
| --- | ---: | ---: | ---: | ---: |
| 4 x 4 KiB | 99.4 us | 42.3 us | 42,603 | 39,896 |
| 256 x 4 KiB | 4.69 ms | 1.35 ms | 1,574,031 | 1,449,666 |
| 2 x 512 KiB | 4.21 ms | 1.04 ms | 4,271,446 | 1,090,741 |

The many-message case reduces preparation time by about 71%. These measurements
cover repeated cache-hit preparation, including the remaining Host work; a new
revision still requires a fresh analysis. They exclude model/network latency,
provider request construction and end-to-end multi-agent scheduling. Allocated
bytes are cumulative allocations, not retained heap or RSS. The earlier
`BenchmarkContextHistory` measures lower-level helpers and is not the direct
before comparator for this result.

Focused regressions cover reuse, equivalent numeric revisions with different
snapshot identities, changed tools/images, zero-valued map keys, caller mutation,
explicit-message admission, failed preparation, trim and run completion. Bounded
review identified obsolete-snapshot retention and a benchmark fixture cleanup
defect; both were corrected before final validation and measurement.

Cache evidence: `/var/tmp/noe-context-cache-rQlzrH`. Shared verification with
the Blob/FormData slice is recorded in the
[session handoff](stdlib-handoff.md#previous-continuation--binary-data-contracts-and-context-analysis-reuse).
