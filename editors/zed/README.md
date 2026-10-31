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

```sh
editors/zed/install.sh
```

Then restart Zed. The status bar of a `.goi` file should read **Goi**.

The script compiles the grammar and the extension wasm itself and drops the
result into Zed's `extensions/installed/goi`. It exists because
`zed: install dev extension` currently fails here with
`compiling grammar 'goi'`: Zed asks clang for the deprecated `wasm32-wasi`
triple while its bundled wasi-sdk only ships `wasm32-wasip1`. The script also
creates the `wasm32-wasi` sysroot symlinks that fix that mismatch, so a later
`install dev extension` may work too.

Override the paths if yours differ:

```sh
WASI_SDK_PATH=/path/to/wasi-sdk ZED_EXTENSIONS=~/.local/share/zed/extensions editors/zed/install.sh
```

Re-run the script after editing `languages/goi/*.scm`, `grammar.js` or
`src/lib.rs`.

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
