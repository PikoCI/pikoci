#!/usr/bin/env bash
# One-time build script to produce a vendored CodeMirror 6 bundle.
# Prerequisites: Node.js 18+ and npm.
# Output: pikoci/transport/http/assets/js/codemirror-hcl.min.js
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT="$REPO_ROOT/pikoci/transport/http/assets/js/codemirror-hcl.min.js"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

cd "$TMPDIR"

cat > package.json <<'PKGJSON'
{
  "private": true,
  "dependencies": {
    "@codemirror/view": "^6",
    "@codemirror/state": "^6",
    "@codemirror/commands": "^6",
    "@codemirror/language": "^6",
    "@codemirror/search": "^6",
    "@codemirror/autocomplete": "^6",
    "@codemirror/lint": "^6",
    "@lezer/highlight": "^1",
    "esbuild": "^0.21"
  }
}
PKGJSON

npm install --silent

cat > entry.js <<'ENTRY'
import {EditorView, keymap, lineNumbers, highlightActiveLine, drawSelection} from "@codemirror/view"
import {EditorState, Compartment} from "@codemirror/state"
import {defaultKeymap, indentWithTab, history, historyKeymap} from "@codemirror/commands"
import {bracketMatching, indentOnInput, foldGutter, syntaxHighlighting, defaultHighlightStyle, StreamLanguage, HighlightStyle} from "@codemirror/language"
import {searchKeymap} from "@codemirror/search"
import {closeBrackets, closeBracketsKeymap} from "@codemirror/autocomplete"
import {lintGutter, setDiagnostics} from "@codemirror/lint"
import {tags} from "@lezer/highlight"

window.PikoCM = {
  EditorView,
  EditorState,
  Compartment,
  keymap,
  lineNumbers,
  highlightActiveLine,
  drawSelection,
  defaultKeymap,
  indentWithTab,
  history,
  historyKeymap,
  bracketMatching,
  indentOnInput,
  foldGutter,
  syntaxHighlighting,
  defaultHighlightStyle,
  StreamLanguage,
  searchKeymap,
  closeBrackets,
  closeBracketsKeymap,
  lintGutter,
  setDiagnostics,
  HighlightStyle,
  tags,
}
ENTRY

npx esbuild entry.js --bundle --minify --format=iife --outfile=codemirror-hcl.min.js

cp codemirror-hcl.min.js "$OUT"
echo "Built: $OUT ($(wc -c < "$OUT") bytes)"
