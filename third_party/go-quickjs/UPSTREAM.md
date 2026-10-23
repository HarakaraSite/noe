# Upstream source

This managed module is based on `github.com/go-quickjs/go-quickjs` v0.28.0.
It was copied from the Go module cache at
`/home/agent/go/pkg/mod/github.com/go-quickjs/go-quickjs@v0.28.0`.
The upstream module checksum is `h1:FWZ3BX1T1U+SK7v7hSMVPcLj791/AD27Y6WVNMjq6ek=`
and its go.mod checksum is `h1:FJ8EJrJDGeJzZFYT20ft7iu17egGueuDloWbvwl5Hvc=`.

The upstream `LICENSE` and the repository's `AGENTS.md` are retained. Engine
changes for NOE are local and are not published upstream by this work.

For NOE distribution, this managed source uses the module prefix
`forge.harakara.site/littleisland/noe/third_party/go-quickjs`. This is a
distribution-path change only; the upstream source identity and provenance
above remain unchanged.

The Go module archive omits two nested tool-module manifests required by the
upstream dependency-boundary tests. These files were restored byte-for-byte
from the v0.28.0 tag (verified after recovery):

- `internal/cmd/v8bench/goja/go.mod`: SHA-256 `23b5bc747bc52209c1f3f7f82a85dd55c694865a4bf2d8e2cabec5dbe8d9b54d`
- `internal/jit/verify/go.mod`: SHA-256 `0d46bf388d48dfafa469690702e1c768db614904470c770bcd5e515dffd91940`

These hashes identify the restored upstream originals. For managed-module
distribution, their module declarations now use the managed prefix above.

The consumer artifact helper excludes these nested modules from the module
archive, matching Go module packaging rules. The managed source retains them
for the upstream tests; no dependency test was skipped or weakened.
