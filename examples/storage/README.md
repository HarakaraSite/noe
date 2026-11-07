# Web Storage

[Examples](../README.md) · [Global API reference](../../internal/apidocs/content/stdlib-globals.md#storage)

Run these examples from the repository root with a NOE executable built from
the matching source. Web Storage is an unreleased source addition after
v2.18.0; installed v2.18.0 executables do not provide it.

```sh
./noe run examples/storage/web-storage.js
```

This writes and reads a value in the current runtime's `sessionStorage`. Each
execution starts with an empty session store. With no local-storage option,
the script explains how to enable its persistent counter and does not create
a storage file.

```sh
./noe run --local-storage /tmp/noe-web-storage.json examples/storage/web-storage.js
```

The selected file's parent directory must already exist. Repeating the second
command increments the counter stored in that JSON file; use a different path
to start a separate store. The path is subject to the selected filesystem's
Root, Cwd, and ReadOnly policy. No credentials or network access are needed.
