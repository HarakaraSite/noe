# mdcli-derived Markdown rendering

Source: [ClarifiedLabs/mdcli v0.0.5](https://github.com/ClarifiedLabs/mdcli/tree/v0.0.5),
commit `8529398a481df65a03c3c86c9ef69aacac2473c4`.
The upstream MIT license is retained in `LICENSE`.

The upstream renderer is an `internal` package, so NOE keeps a source snapshot
of `internal/markdown` and its standard-library-only `internal/highlight`
dependency, including their tests. The CLI, pager, Mermaid renderer, and
terminal discovery are not included. NOE always uses `ANSI: false`.
This is a terminal-friendly Markdown subset, not a full CommonMark parser.

Local adaptations:

- Imports point at `noe/internal/mdcli`.
- Column measurement and hard wrapping account for Japanese wide characters and
  combining marks. These width rules follow NOE's existing qjs-derived editor;
  its upstream MIT license is retained in `LICENSE.quickjs`. Compound emoji
  widths can still depend on the terminal.

Human observation renders accepted agent messages and final answers only.
Thinking fragments and stderr stay plain; stdout is hidden from the human view. Recorded JSONL and
`observe_agent` results retain their original text.

When updating the snapshot, compare these two packages with the pinned source,
retain the width adaptations, and run their tests and NOE's observation tests.
