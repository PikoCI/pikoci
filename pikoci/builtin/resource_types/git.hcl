resource_type "git" {
  cache = true
  params = [
    "url",
    "branch",
    "name",
    "token",
    "pr",
    "tag",
    "provider",
  ]
  check "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      URL="$param_url"
      BRANCH="$${param_branch:-HEAD}"
      TOKEN="$param_token"
      PR="$param_pr"
      TAG="$param_tag"
      PROVIDER="$param_provider"

      # Resolve provider
      if [ -n "$PROVIDER" ]; then
        case "$PROVIDER" in
          github|gitlab|gitea) ;;
          forgejo) PROVIDER="gitea" ;;
          *) echo "error: invalid provider '$PROVIDER' (must be github, gitlab, gitea, or forgejo)" >&2; exit 1 ;;
        esac
      else
        if echo "$URL" | grep -q "github.com"; then
          PROVIDER="github"
        elif echo "$URL" | grep -q "gitlab.com"; then
          PROVIDER="gitlab"
        fi
      fi

      # Extract host and repo path from URL
      HOST=$(echo "$URL" | sed -E 's|https?://([^/]+).*|\1|')
      REPO_PATH=$(echo "$URL" | sed -E 's|https?://[^/]+/||;s|\.git$||')
      PROJECT=$(echo "$REPO_PATH" | sed 's|/|%2F|g')

      # Maintain a bare clone in CACHE_DIR for faster operations
      if [ -n "$CACHE_DIR" ]; then
        if [ -n "$TOKEN" ]; then
          CACHE_URL=$(echo "$URL" | sed -E "s|https://|https://oauth2:$TOKEN@|")
        else
          CACHE_URL="$URL"
        fi
        if [ -d "$CACHE_DIR/repo" ]; then
          git -C "$CACHE_DIR/repo" remote set-url origin "$CACHE_URL" 2>/dev/null || true
          git -C "$CACHE_DIR/repo" fetch --prune 2>/dev/null || true
        else
          git clone --bare "$CACHE_URL" "$CACHE_DIR/repo" 2>/dev/null || true
        fi
      fi

      # First check: version vars are empty when no previous versions exist.
      # Return only the latest item to avoid triggering a build per historical entry.

      # Tag mode: check for latest tags
      if [ "$TAG" = "true" ]; then
        if [ -z "$TOKEN" ]; then
          echo "error: tag=true requires a token" >&2
          exit 1
        fi

        # On first check return only the latest tag
        if [ -z "$version_tag" ]; then
          PER_PAGE=1
        else
          PER_PAGE=100
        fi

        # GitHub tags
        if [ "$PROVIDER" = "github" ]; then
          curl -sf -H "Authorization: token $TOKEN" \
            "https://api.github.com/repos/$REPO_PATH/tags?per_page=$PER_PAGE" \
            | jq -c --arg known "$version_tag" \
              'map({"ref": .commit.sha, "tag": .name})
               | if $known == "" then .
                 else reduce .[] as $t ({out:[], done:false};
                   if .done then . else (if $t.tag == $known then .done=true else .out += [$t] end) end
                 ) | .out
                 end'
          exit 0
        fi

        # GitLab tags
        if [ "$PROVIDER" = "gitlab" ]; then
          curl -sf -H "PRIVATE-TOKEN: $TOKEN" \
            "https://$HOST/api/v4/projects/$PROJECT/repository/tags?order_by=updated&sort=desc&per_page=$PER_PAGE" \
            | jq -c --arg known "$version_tag" \
              'map({"ref": .commit.id, "tag": .name})
               | if $known == "" then .
                 else reduce .[] as $t ({out:[], done:false};
                   if .done then . else (if $t.tag == $known then .done=true else .out += [$t] end) end
                 ) | .out
                 end'
          exit 0
        fi

        # Gitea/Forgejo tags
        if [ "$PROVIDER" = "gitea" ]; then
          curl -sf -H "Authorization: token $TOKEN" \
            "https://$HOST/api/v1/repos/$REPO_PATH/tags?limit=$PER_PAGE" \
            | jq -c --arg known "$version_tag" \
              'map({"ref": .commit.sha, "tag": .name})
               | if $known == "" then .
                 else reduce .[] as $t ({out:[], done:false};
                   if .done then . else (if $t.tag == $known then .done=true else .out += [$t] end) end
                 ) | .out
                 end'
          exit 0
        fi

        # Fallback: try GitLab API, then Gitea API
        RESULT=$(curl -sf -H "PRIVATE-TOKEN: $TOKEN" \
          "https://$HOST/api/v4/projects/$PROJECT/repository/tags?order_by=updated&sort=desc&per_page=$PER_PAGE" 2>/dev/null \
          | jq -c --arg known "$version_tag" \
            'map({"ref": .commit.id, "tag": .name})
             | if $known == "" then .
               else reduce .[] as $t ({out:[], done:false};
                 if .done then . else (if $t.tag == $known then .done=true else .out += [$t] end) end
               ) | .out
               end' 2>/dev/null) && [ -n "$RESULT" ] && echo "$RESULT" && exit 0

        RESULT=$(curl -sf -H "Authorization: token $TOKEN" \
          "https://$HOST/api/v1/repos/$REPO_PATH/tags?limit=$PER_PAGE" 2>/dev/null \
          | jq -c --arg known "$version_tag" \
            'map({"ref": .commit.sha, "tag": .name})
             | if $known == "" then .
               else reduce .[] as $t ({out:[], done:false};
                 if .done then . else (if $t.tag == $known then .done=true else .out += [$t] end) end
               ) | .out
               end' 2>/dev/null) && [ -n "$RESULT" ] && echo "$RESULT" && exit 0

        echo "error: tag=true is not supported for this provider (use provider param to specify github, gitlab, gitea, or forgejo)" >&2
        exit 1
      fi

      # PR mode: check for open pull requests
      if [ "$PR" = "true" ]; then
        if [ -z "$TOKEN" ]; then
          echo "error: pr=true requires a token" >&2
          exit 1
        fi

        # On first check return only the most recently updated PR
        if [ -z "$version_pr" ]; then
          PER_PAGE=1
        else
          PER_PAGE=100
        fi

        # GitHub PRs
        if [ "$PROVIDER" = "github" ]; then
          curl -sf -H "Authorization: token $TOKEN" \
            "https://api.github.com/repos/$REPO_PATH/pulls?state=open&sort=updated&direction=desc&per_page=$PER_PAGE" \
            | jq -c '[.[] | {"ref": .head.sha, "pr": (.number | tostring)}]'
          exit 0
        fi

        # GitLab MRs
        if [ "$PROVIDER" = "gitlab" ]; then
          curl -sf -H "PRIVATE-TOKEN: $TOKEN" \
            "https://$HOST/api/v4/projects/$PROJECT/merge_requests?state=opened&order_by=updated_at&sort=desc&per_page=$PER_PAGE" \
            | jq -c '[.[] | {"ref": .sha, "pr": (.iid | tostring)}]'
          exit 0
        fi

        # Gitea/Forgejo PRs
        if [ "$PROVIDER" = "gitea" ]; then
          curl -sf -H "Authorization: token $TOKEN" \
            "https://$HOST/api/v1/repos/$REPO_PATH/pulls?state=open&sort=recentupdate&limit=$PER_PAGE" \
            | jq -c '[.[] | {"ref": .head.sha, "pr": (.number | tostring)}]'
          exit 0
        fi

        # Fallback: try GitLab API, then Gitea API
        RESULT=$(curl -sf -H "PRIVATE-TOKEN: $TOKEN" \
          "https://$HOST/api/v4/projects/$PROJECT/merge_requests?state=opened&order_by=updated_at&sort=desc&per_page=$PER_PAGE" 2>/dev/null \
          | jq -c '[.[] | {"ref": .sha, "pr": (.iid | tostring)}]' 2>/dev/null) && [ -n "$RESULT" ] && echo "$RESULT" && exit 0

        RESULT=$(curl -sf -H "Authorization: token $TOKEN" \
          "https://$HOST/api/v1/repos/$REPO_PATH/pulls?state=open&sort=recentupdate&limit=$PER_PAGE" 2>/dev/null \
          | jq -c '[.[] | {"ref": .head.sha, "pr": (.number | tostring)}]' 2>/dev/null) && [ -n "$RESULT" ] && echo "$RESULT" && exit 0

        echo "error: pr=true is not supported for this provider (use provider param to specify github, gitlab, gitea, or forgejo)" >&2
        exit 1
      fi

      # Commit mode: GitHub API
      if [ -n "$TOKEN" ] && [ "$PROVIDER" = "github" ]; then
        REF=$(curl -sf -H "Authorization: token $TOKEN" \
          "https://api.github.com/repos/$REPO_PATH/commits?sha=$BRANCH&per_page=1" \
          | jq -r '.[0].sha')
        if [ -n "$REF" ] && [ "$REF" != "null" ]; then
          echo "[{\"ref\":\"$REF\"}]"
          exit 0
        fi
      fi

      # Commit mode: GitLab API
      if [ -n "$TOKEN" ] && [ "$PROVIDER" = "gitlab" ]; then
        REF=$(curl -sf -H "PRIVATE-TOKEN: $TOKEN" \
          "https://$HOST/api/v4/projects/$PROJECT/repository/commits?ref_name=$BRANCH&per_page=1" \
          | jq -r '.[0].id')
        if [ -n "$REF" ] && [ "$REF" != "null" ]; then
          echo "[{\"ref\":\"$REF\"}]"
          exit 0
        fi
      fi

      # Commit mode: Gitea API
      if [ -n "$TOKEN" ] && [ "$PROVIDER" = "gitea" ]; then
        REF=$(curl -sf -H "Authorization: token $TOKEN" \
          "https://$HOST/api/v1/repos/$REPO_PATH/commits?sha=$BRANCH&limit=1" \
          | jq -r '.[0].sha')
        if [ -n "$REF" ] && [ "$REF" != "null" ]; then
          echo "[{\"ref\":\"$REF\"}]"
          exit 0
        fi
      fi

      # Commit mode: fallback chain for unknown providers
      if [ -n "$TOKEN" ] && [ -z "$PROVIDER" ]; then
        # Try GitLab API
        REF=$(curl -sf -H "PRIVATE-TOKEN: $TOKEN" \
          "https://$HOST/api/v4/projects/$PROJECT/repository/commits?ref_name=$BRANCH&per_page=1" 2>/dev/null \
          | jq -r '.[0].id' 2>/dev/null)
        if [ -n "$REF" ] && [ "$REF" != "null" ]; then
          echo "[{\"ref\":\"$REF\"}]"
          exit 0
        fi

        # Try Gitea API
        REF=$(curl -sf -H "Authorization: token $TOKEN" \
          "https://$HOST/api/v1/repos/$REPO_PATH/commits?sha=$BRANCH&limit=1" 2>/dev/null \
          | jq -r '.[0].sha' 2>/dev/null)
        if [ -n "$REF" ] && [ "$REF" != "null" ]; then
          echo "[{\"ref\":\"$REF\"}]"
          exit 0
        fi
      fi

      # Fallback: git ls-remote
      if [ -n "$TOKEN" ]; then
        REF=$(git -c credential.helper="!f() { echo password=$TOKEN; }; f" ls-remote "$URL" "$BRANCH" | awk '{print $1}')
      else
        REF=$(git ls-remote "$URL" "$BRANCH" | awk '{print $1}')
      fi
      if [ -z "$REF" ]; then
        REF=$(git ls-remote "$URL" HEAD | awk '{print $1}')
      fi
      echo "[{\"ref\":\"$REF\"}]"
      EOT
    ]
  }
  pull "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      URL="$param_url"
      TOKEN="$param_token"
      BRANCH="$param_branch"
      PR="$param_pr"
      TAG="$param_tag"

      # Determine provider for PR ref format
      PROVIDER="$param_provider"
      if [ "$PROVIDER" = "forgejo" ]; then PROVIDER="gitea"; fi
      if [ -z "$PROVIDER" ]; then
        if echo "$URL" | grep -q "github.com"; then PROVIDER="github"
        elif echo "$URL" | grep -q "gitlab.com"; then PROVIDER="gitlab"
        else PROVIDER="gitea"
        fi
      fi

      # Inject token into HTTPS URL if provided
      if [ -n "$TOKEN" ]; then
        URL=$(echo "$URL" | sed -E "s|https://|https://oauth2:$TOKEN@|")
      fi

      # Use local cache as reference for faster clones
      REFERENCE_ARGS=""
      if [ -n "$CACHE_DIR" ] && [ -d "$CACHE_DIR/repo" ]; then
        git -C "$CACHE_DIR/repo" fetch --prune 2>/dev/null || true
        REFERENCE_ARGS="--reference-if-able $CACHE_DIR/repo"
      fi

      if [ "$TAG" = "true" ] && [ -n "$version_tag" ]; then
        # Tag mode: clone at the specific tag
        git clone $REFERENCE_ARGS -b "$version_tag" --depth 1 "$URL" "$param_name"
      elif [ "$PR" = "true" ] && [ -n "$version_pr" ]; then
        # PR mode: fetch the PR head ref
        git clone $REFERENCE_ARGS "$URL" "$param_name"
        cd "$param_name"
        if [ "$PROVIDER" = "gitlab" ]; then
          git fetch origin "merge-requests/$version_pr/head:pr-$version_pr"
        else
          git fetch origin "pull/$version_pr/head:pr-$version_pr"
        fi
        git checkout "pr-$version_pr"
      elif [ -n "$BRANCH" ]; then
        git clone $REFERENCE_ARGS -b "$BRANCH" "$URL" "$param_name"
        cd "$param_name"
        git checkout "$version_ref"
      else
        git clone $REFERENCE_ARGS "$URL" "$param_name"
        cd "$param_name"
        git checkout "$version_ref"
      fi
      EOT
    ]
  }
  push "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      URL="$param_url"
      TOKEN="$param_token"

      cd "$param_name"
      if [ -n "$TOKEN" ]; then
        REMOTE_URL=$(echo "$URL" | sed -E "s|https://|https://oauth2:$TOKEN@|")
        git remote set-url origin "$REMOTE_URL"
      fi
      git push
      EOT
    ]
  }
}
