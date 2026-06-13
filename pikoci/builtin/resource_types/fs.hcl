resource_type "fs" {
  params = [
    "path",
  ]
  check "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      TARGET="$param_path"
      if [ ! -e "$TARGET" ]; then
        echo "[]"
        exit 0
      fi
      if [ -f "$TARGET" ]; then
        HASH=$(sha256sum "$TARGET" | cut -d' ' -f1)
        MOD=$(stat -c '%Y' "$TARGET" 2>/dev/null || stat -f '%m' "$TARGET")
        SIZE=$(stat -c '%s' "$TARGET" 2>/dev/null || stat -f '%z' "$TARGET")
      else
        HASH=$(find "$TARGET" -type f | sort | xargs sha256sum 2>/dev/null | sha256sum | cut -d' ' -f1)
        MOD=""
        SIZE=""
      fi
      PREV="$${version_hash:-}"
      if [ "$HASH" = "$PREV" ]; then
        echo "[]"
        exit 0
      fi
      if [ -n "$MOD" ]; then
        echo "[{\"path\":\"$TARGET\",\"hash\":\"$HASH\",\"modified\":\"$MOD\",\"size\":\"$SIZE\"}]"
      else
        echo "[{\"path\":\"$TARGET\",\"hash\":\"$HASH\"}]"
      fi
      EOT
    ]
  }
  pull "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      TARGET="$param_path"
      if [ -f "$TARGET" ]; then
        cp "$TARGET" "$WORKDIR/"
      elif [ -d "$TARGET" ]; then
        cp -r "$TARGET/." "$WORKDIR/"
      else
        echo "error: path not found: $TARGET" >&2
        exit 1
      fi
      EOT
    ]
  }
  push "exec" { }
}
