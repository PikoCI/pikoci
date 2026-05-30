notification_type "github-check" {
  params = [
    "app_id",
    "installation_id",
    "private_key",
    "repository",
    "base_url",
  ]
  notify "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      APP_ID="$param_app_id"
      INSTALL_ID="$param_installation_id"
      PRIVATE_KEY="$param_private_key"
      REPO="$param_repository"

      STATUS="$notify_status"
      CONCLUSION="$notify_conclusion"
      HEAD_SHA="$notify_head_sha"
      CHECK_NAME="$notify_name"
      DETAILS_URL="$notify_details_url"
      BASE_URL="$param_base_url"
      BASE_URL="$${BASE_URL%/}"

      # Default details URL from base_url and build metadata
      if [ -z "$DETAILS_URL" ] && [ -n "$BASE_URL" ]; then
        DETAILS_URL="$${BASE_URL}/teams/$${BUILD_TEAM_NAME}/pipelines/$${BUILD_PIPELINE_NAME}/jobs/$${BUILD_JOB_NAME}/builds/$${BUILD_NUMBER}"
      fi

      # Default check name from build metadata
      if [ -z "$CHECK_NAME" ]; then
        CHECK_NAME="$BUILD_PIPELINE_NAME/$BUILD_JOB_NAME"
      fi

      # Default head SHA from git if not provided.
      # Try any git repo found in WORKDIR (the get step clones there).
      if [ -z "$HEAD_SHA" ]; then
        for d in "$WORKDIR"/*/; do
          if [ -d "$d/.git" ]; then
            HEAD_SHA=$(git -C "$d" rev-parse HEAD 2>/dev/null || true)
            break
          fi
        done
      fi

      if [ -z "$HEAD_SHA" ]; then
        echo "error: no head_sha provided and git rev-parse HEAD failed" >&2
        exit 1
      fi

      # Write private key to temp file (use WORKDIR since /tmp may be read-only)
      KEY_FILE="$WORKDIR/.github-check-key.pem"
      trap 'rm -f "$KEY_FILE"' EXIT
      printf '%s' "$PRIVATE_KEY" > "$KEY_FILE"

      # Generate JWT (RS256)
      NOW=$(date +%s)
      IAT=$((NOW - 60))
      EXP=$((NOW + 600))

      HEADER=$(printf '{"alg":"RS256","typ":"JWT"}' | openssl base64 -e -A | tr '+/' '-_' | tr -d '=')
      PAYLOAD=$(printf '{"iat":%d,"exp":%d,"iss":"%s"}' "$IAT" "$EXP" "$APP_ID" | openssl base64 -e -A | tr '+/' '-_' | tr -d '=')
      SIGNATURE=$(printf '%s.%s' "$HEADER" "$PAYLOAD" | openssl dgst -sha256 -sign "$KEY_FILE" -binary | openssl base64 -e -A | tr '+/' '-_' | tr -d '=')
      JWT="$HEADER.$PAYLOAD.$SIGNATURE"

      # Exchange JWT for installation token
      TOKEN=$(curl -sf -X POST \
        -H "Authorization: Bearer $JWT" \
        -H "Accept: application/vnd.github+json" \
        "https://api.github.com/app/installations/$INSTALL_ID/access_tokens" \
        | jq -r '.token')

      if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
        echo "error: failed to obtain installation token" >&2
        exit 1
      fi

      RESOURCE_NAME=$(echo "$notify_name" | tr ' ' '-')
      if [ -z "$RESOURCE_NAME" ]; then
        RESOURCE_NAME="github-check"
      fi
      ID_FILE="$WORKDIR/.github-check-$${RESOURCE_NAME}.id"

      if [ -n "$STATUS" ] && [ "$STATUS" = "in_progress" ]; then
        # Create a new check run
        BODY=$(jq -n \
          --arg name "$CHECK_NAME" \
          --arg sha "$HEAD_SHA" \
          --arg status "$STATUS" \
          --arg url "$DETAILS_URL" \
          '{name: $name, head_sha: $sha, status: $status} | if $url != "" then . + {details_url: $url} else . end')

        RESPONSE=$(curl -sf -X POST \
          -H "Authorization: token $TOKEN" \
          -H "Accept: application/vnd.github+json" \
          "https://api.github.com/repos/$REPO/check-runs" \
          -d "$BODY")

        CHECK_RUN_ID=$(echo "$RESPONSE" | jq -r '.id')
        echo "$CHECK_RUN_ID" > "$ID_FILE"
        echo "Created check run $CHECK_RUN_ID for $CHECK_NAME"
      elif [ -n "$CONCLUSION" ]; then
        # Update existing check run with conclusion
        CHECK_RUN_ID=""
        if [ -f "$ID_FILE" ]; then
          CHECK_RUN_ID=$(cat "$ID_FILE")
        fi

        if [ -z "$CHECK_RUN_ID" ]; then
          echo "error: no check run ID found, create one first with status=in_progress" >&2
          exit 1
        fi

        BODY=$(jq -n \
          --arg status "completed" \
          --arg conclusion "$CONCLUSION" \
          --arg url "$DETAILS_URL" \
          '{status: $status, conclusion: $conclusion} | if $url != "" then . + {details_url: $url} else . end')

        curl -sf -o /dev/null -X PATCH \
          -H "Authorization: token $TOKEN" \
          -H "Accept: application/vnd.github+json" \
          "https://api.github.com/repos/$REPO/check-runs/$CHECK_RUN_ID" \
          -d "$BODY"

        echo "Updated check run $CHECK_RUN_ID with conclusion=$CONCLUSION"
      else
        echo "error: either notify_status or notify_conclusion must be set" >&2
        exit 1
      fi
      EOT
    ]
  }
}
