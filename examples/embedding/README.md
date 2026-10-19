# Embed NOE's generic APIs inside this repository

Run from the repository root with Go 1.27.2 or later:

```sh
CGO_ENABLED=0 go run ./examples/embedding
```

This Go host creates a go-quickjs runtime and an event loop, installs a selected
configuration through `internal/noestdlib`, looks up the built-in English API
reference, and pipes borrowed input to borrowed output using Web Streams. It
imports neither NOE's CLI nor its agent or provider packages. No credentials or
external services are required.

The packages are still **internal**. This example works inside NOE's Go module;
it is not yet a public Go library that another module can import. Public package
paths and stable embedding interfaces belong to the next stage.

`Core` retains `stdlib.Config` capabilities. Nil FS, Fetch, DNS, Run, Serve and
Sockets fields omit their corresponding core capabilities. `Docs: true` adds
`noe:docs` independently. Its catalog describes the full NOE API reference,
including APIs that this particular host has not installed; it is not a capability
introspection service.

A non-nil `IO` explicitly enables all five native modules: `noe:io`,
`noe:process`, `noe:fs-handle`, `noe:sockets` and `noe:signals`. This group has the
host user's OS authority. Core FS/Fetch restrictions do not constrain it.
It requires Web APIs and a non-nil `Core.Process`, since `spawn` reads the
JavaScript `process.env`, `process.platform` and `process.cwd()`. Invalid
combinations are rejected before registration; the installer does not implicitly
grant a Process object or copy the host environment. `IO.Cwd` sets native path
resolution, while spawn's default cwd comes from `process.cwd()`. Set them
explicitly when the same directory is desired, as this example does.

The caller owns and drives the runtime and loop. Close both at host exit:
`Loop.Close()` alone does not close the runtime or all native resources.
The installer owns its output adapters and native resources, and closes them
with the runtime. It borrows input and output destinations, so the original
streams remain open. Workers borrow the root output boundary, receive their own
native resources, and receive no inherited stdin. The core Worker's existing
`New`, `Load`, `Max` and `Installed` settings remain available to a host that
explicitly enables Workers; this small example does not enable them.

If installation fails after registration has started, close and discard that
runtime. Registration is not transactional, and retrying installation on a
partially configured runtime is unsupported. A separate fresh runtime can reuse
the original configuration.
