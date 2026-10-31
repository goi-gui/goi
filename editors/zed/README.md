# goi for Zed

Syntax highlighting, bracket matching, indentation and an element outline for
`.goi` design files.

## What it gives you

| Feature | Source |
|---|---|
| Highlighting | `languages/goi/highlights.scm` |
| Bracket matching (and rainbow brackets) | `languages/goi/brackets.scm` |
| Indent after `{` | `languages/goi/indents.scm` |
| Outline panel (Window › Column › Card › …) | `languages/goi/outline.scm` |
| Language name, `.goi` suffix, `//` comments | `languages/goi/config.toml` |

Element names read as types, property names as fields, and value identifiers
(`filled`, `primary`, `dark_mode`) as constants, because those are names the
framework resolves rather than variables.

## Layout

```
editors/zed/
├── extension.toml           # extension manifest, wires the grammar to the language
├── src/lib.rs               # empty extension: no LSP, no commands
├── languages/goi/           # language config and tree-sitter queries
└── tree-sitter-goi/         # the grammar (grammar.js + generated src/parser.c)
```

## Install locally

The grammar is cloned from a git URL, so the grammar directory must be its own
repository. It already is; before installing, make sure the commit in
`extension.toml` matches:

```sh
cd editors/zed/tree-sitter-goi
git rev-parse --short HEAD     # must equal `rev` under [grammars.goi]
```

Then in Zed: `zed: install dev extension` and pick `editors/zed`.

Rebuild the extension wasm after changing `src/lib.rs`:

```sh
cd editors/zed
cargo build --release --target wasm32-wasip2
cp target/wasm32-wasip2/release/goi.wasm extension.wasm
```

## Regenerating the grammar

The parser is checked in, so a fresh clone needs no Node. After editing
`grammar.js`:

```sh
cd editors/zed/tree-sitter-goi
tree-sitter generate --abi 14   # 14, not 15: Zed's runtime accepts up to 14
tree-sitter test                # corpus in test/corpus/
tree-sitter parse ../../../examples/*/*.goi   # every design must parse with no ERROR
```

`--abi 14` matters: generating with the CLI default (15) makes Zed fail the
install with `compiling grammar 'goi'`.

## Publishing

Replace the `file://` repository in `extension.toml` with a real git URL that
contains the grammar at the top level (or add `path = "…"` inside that repo) and
pin a `rev`. Then follow Zed's extension publishing steps.

## Keeping it in sync with the language

The grammar mirrors `docs/SYNTAX.md`. If you add a literal form, an operator or
a declaration, update `grammar.js`, regenerate, and add a case to
`test/corpus/basic.txt` so the next change cannot silently break highlighting.
