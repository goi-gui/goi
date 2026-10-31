#!/usr/bin/env bash
# Install the goi extension into Zed without Zed's grammar build step.
#
# Why this exists: `zed: install dev extension` compiles the tree-sitter grammar
# with the WASI SDK Zed downloads, and some Zed builds ask clang for the
# deprecated `wasm32-wasi` triple while their bundled sysroot only ships
# `wasm32-wasip1`. That fails with "compiling grammar 'goi'". This script
# compiles the grammar itself and places the result where Zed expects an
# already-built extension.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
dest="${ZED_EXTENSIONS:-$HOME/.local/share/zed/extensions}/installed/goi"
sdk="${WASI_SDK_PATH:-$HOME/.local/share/zed/extensions/build/wasi-sdk}"

if [ ! -x "$sdk/bin/clang" ]; then
	echo "no wasi-sdk at $sdk; set WASI_SDK_PATH" >&2
	exit 1
fi

# Zed's sysroot ships wasm32-wasip1 only; its clang still asks for wasm32-wasi.
for sub in include lib; do
	link="$sdk/share/wasi-sysroot/$sub/wasm32-wasi"
	[ -e "$link" ] || ln -s wasm32-wasip1 "$link"
done

echo "building the grammar"
"$sdk/bin/clang" --target=wasm32-wasi --sysroot="$sdk/share/wasi-sysroot" \
	-fPIC -shared -O2 -I"$here/tree-sitter-goi/src" \
	"$here/tree-sitter-goi/src/parser.c" -o "$here/tree-sitter-goi/goi.wasm"

echo "building the extension"
(cd "$here" && cargo build --release --target wasm32-wasip2 --quiet)

echo "installing into $dest"
rm -rf "$dest"
mkdir -p "$dest/grammars" "$dest/languages/goi"
cp "$here/extension.toml" "$dest/"
cp "$here/languages/goi/"* "$dest/languages/goi/"
cp "$here/tree-sitter-goi/goi.wasm" "$dest/grammars/goi.wasm"
cp "$here/target/wasm32-wasip2/release/goi.wasm" "$dest/extension.wasm"

echo "done. restart Zed and open a .goi file; the status bar should read \"Goi\"."
