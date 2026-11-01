# goi for Zed

Syntax highlighting, bracket matching, indentation, an element outline and a
language server for `.goi` design files.

## What it gives you

| Feature | Source |
|---|---|
| Highlighting | `languages/goi/highlights.scm` |
| Bracket matching (and rainbow brackets) | `languages/goi/brackets.scm` |
| Indent after `{` | `languages/goi/indents.scm` |
| Outline panel (Window › Column › Card › …) | `languages/goi/outline.scm` |
| Language name, `.goi` suffix, `//` comments | `languages/goi/config.toml` |
| Diagnostics, completion, hover, go-to-definition | `cmd/goipls` |
| Icon name completion (Material Symbols) | `cmd/goipls` + `internal/text` |
| `app.` / `window.` global completion | `cmd/goipls` |

Element names read as types, property names as fields, and value identifiers
(`filled`, `primary`, `dark_mode`) as constants, because those are names the
framework resolves rather than variables.

The language server is the same parser and schema the runtime uses, so a design
that draws also type-checks: diagnostics land on the property they blame, even
when it lives in an imported fragment. Completion knows the element you are
inside, the values a property accepts, the theme's colour roles and the `id`s the
file declares. An `icon`/`trailingIcon`/`iconOn`/`iconOff` value completes to the
names the embedded Material Symbols font actually has, read out of the woff, so
the list cannot drift from what draws. `Ctrl`-click on an `import` path or its
alias opens the fragment.

## Language server

The server is a Go binary, so it is not bundled with the extension. Install it
once and keep it on `PATH` (`~/.local/bin` is fine):

```sh
go install github.com/ksckaan1/goi/cmd/goipls@latest
```

If Zed shows "goipls not found on PATH" in the language server menu, that
command has not run or its directory is not on `PATH`. Restart the server from
that menu after reinstalling.

`goipls` speaks Language Server Protocol over stdio, so any editor that can start
a stdio server can use it.

## Layout

```
editors/zed/
├── extension.toml           # extension manifest: grammar + language server
├── src/lib.rs               # starts goipls from PATH; no other commands
├── languages/goi/           # language config and tree-sitter queries
└── tree-sitter-goi/         # the grammar (grammar.js + generated src/parser.c)
```

The server itself is `cmd/goipls` (transport and features live in
`internal/lsp`).

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
