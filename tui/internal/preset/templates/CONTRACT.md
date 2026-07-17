# TUI `init.jsonc` consumer-copy contract

The sibling [`init.jsonc`](init.jsonc) is the TUI consumer/embed/example/new-config
copy of the kernel-owned canonical file. It must remain byte-identical to all
three canonical kernel sources:

- [`src/lingtai/init.jsonc`](https://github.com/Lingtai-AI/lingtai-kernel/blob/main/src/lingtai/init.jsonc) — the sole shape source;
- [`src/lingtai/CONTRACT.md`](https://github.com/Lingtai-AI/lingtai-kernel/blob/main/src/lingtai/CONTRACT.md) — compatibility purpose, behavior, and retirement promise;
- [`src/lingtai/ANATOMY.md`](https://github.com/Lingtai-AI/lingtai-kernel/blob/main/src/lingtai/ANATOMY.md) — exact reader/writer/validator/Nudge code routes.

## Ownership

The kernel owns canonical and compatibility semantics, reader promises, conflict
handling, retirement policy, and the real reader/validator/effective-config
implementation. TUI owns only a byte-identical copy for embedding, examples, and
new-config creation; every new or explicit TUI write emits canonical `shell`.

TUI never automatically rewrites an existing `init.json`. It has no migration,
version, or stored-progress chain for this capability shape. Legacy input is
handled by the kernel reader/Nudge contract; explicit TUI writers fail closed on
a differing `bash` + `shell` pair rather than silently merging it.

The sibling `examples/init.jsonc` is checked byte-for-byte by the local preset
sync test. Integration/PR validation must additionally compare the kernel
canonical file, this TUI template, and the example with `cmp -s`. Any byte drift
is a mechanical failure, not a semantic-review exception.
