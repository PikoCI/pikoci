resource_type "artifact" {
  cache = true
  params = [
    "dir",
    "base_dir",
  ]
  check "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      VERSIONS_DIR="$CACHE_DIR/versions"
      if [ ! -d "$VERSIONS_DIR" ]; then
        echo "[]"
        exit 0
      fi
      PREV_TS="$${version_timestamp:-}"
      if [ -z "$PREV_TS" ]; then
        # First check: return only latest
        LATEST=$(ls -1 "$VERSIONS_DIR"/*.json 2>/dev/null | sort | tail -1)
        if [ -z "$LATEST" ]; then echo "[]"; exit 0; fi
        echo "[$(cat "$LATEST")]"
      else
        # Return all versions newer than previous timestamp
        RESULT="["
        FIRST=true
        for f in $(ls -1 "$VERSIONS_DIR"/*.json 2>/dev/null | sort); do
          FILE_TS=$(cat "$f" | grep -o '"timestamp":"[^"]*"' | cut -d'"' -f4)
          if [ "$FILE_TS" \> "$PREV_TS" ]; then
            if [ "$FIRST" = true ]; then FIRST=false; else RESULT="$RESULT,"; fi
            RESULT="$RESULT$(cat "$f")"
          fi
        done
        echo "$${RESULT}]"
      fi
      EOT
    ]
  }
  pull "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      STORE="$${param_base_dir:-$CACHE_DIR}"
      DEST="$param_dir"
      TARBALL="$STORE/data/$${version_sha}.tar.gz"
      if [ ! -f "$TARBALL" ]; then
        echo "error: artifact not found: $TARBALL" >&2
        exit 1
      fi
      mkdir -p "$DEST"
      tar -xzf "$TARBALL" -C "$DEST"
      echo "extracted artifact $version_sha to $DEST"
      EOT
    ]
  }
  push "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      STORE="$${param_base_dir:-$CACHE_DIR}"
      mkdir -p "$STORE/versions" "$STORE/data"
      SRC="$put_dir"
      if [ ! -d "$SRC" ]; then
        echo "error: directory '$SRC' not found" >&2
        exit 1
      fi
      SHA=$(tar -cf - -C "$SRC" . | sha256sum | cut -d' ' -f1)
      TARBALL="$STORE/data/$${SHA}.tar.gz"
      if [ ! -f "$TARBALL" ]; then
        tar -czf "$TARBALL" -C "$SRC" .
      fi
      TS=$(date -u +%Y%m%d%H%M%S%N)
      echo "{\"sha\":\"$SHA\",\"timestamp\":\"$TS\"}" > "$STORE/versions/$${TS}-$${SHA}.json"
      echo "[{\"sha\":\"$SHA\",\"timestamp\":\"$TS\"}]"
      EOT
    ]
  }
}
