# Low-level API group 2: planning spikes

These opt-in prototypes check the design before NOE's file-handle, TCP/Unix
socket, and OS-signal implementation plan is finalized. They do not register
production modules or change NOE's CLI, pinned stdlib, or dependencies.

| Spike | Evidence |
| --- | --- |
| [File handles](file/README.md) | [Results](file/RESULTS.md): positional reads/writes versus existing range streams, EOF, partial reads, owner delivery, and handle lifetime |
| [TCP / Unix sockets](net/README.md) | [Results](net/RESULTS.md): duplex Web Streams, half-close, cancellation, keepalive, and Unix pathname behavior |
| [OS signals](signals/README.md) | [Results](signals/RESULTS.md): real main/Worker delivery, default actions, existing cancellation, and subscription lifetime |

Run from the repository root on Linux:

```sh
go test -tags=lowlevel2_spike -count=1 -v ./spikes/low-level-group2/...
go test -race -tags=lowlevel2_spike -count=1 ./spikes/low-level-group2/...
go vet -tags=lowlevel2_spike ./spikes/low-level-group2/...
```

The signal spike is Linux-only; the other results are also Linux observations.
Real signals and forced termination target helper children created by the
suite. The tests use temporary local files/sockets, no external service or
credentials, and no real model provider. Default builds and tests omit these
experimental packages.

These are embedded go-quickjs/stdlib experiments, including an actual Worker
in the signal spike. They do not establish NOE CLI/REPL/run_js integration or
performance. Prototype API shapes differ from the proposed production API.

See the [plan](../../docs/development/low-level-group2-plan.md),
[combined spike record](../../docs/development/low-level-group2-spike.md), and
[plan reviews](../../docs/development/low-level-group2-review.md).
