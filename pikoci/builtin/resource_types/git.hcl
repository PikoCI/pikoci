resource_type "git" {
  params = [
    "url",
    "branch",
    "name",
    "token",
    "pr",
    "tag",
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

      # Tag mode: check for latest tags
      if [ "$TAG" = "true" ]; then
        if [ -z "$TOKEN" ]; then
          echo "error: tag=true requires a token" >&2
          exit 1
        fi

        # GitHub tags
        if echo "$URL" | grep -q "github.com"; then
          REPO=$(echo "$URL" | sed -E 's|https?://github\.com/||;s|\.git$||')
          curl -sf -H "Authorization: token $TOKEN" \
            "https://api.github.com/repos/$REPO/tags?per_page=100" \
            | jq -c '[.[] | {"ref": .commit.sha, "tag": .name}]'
          exit 0
        fi

        # GitLab tags
        if echo "$URL" | grep -q "gitlab.com"; then
          PROJECT=$(echo "$URL" | sed -E 's|https?://gitlab\.com/||;s|\.git$||' | sed 's|/|%2F|g')
          curl -sf -H "PRIVATE-TOKEN: $TOKEN" \
            "https://gitlab.com/api/v4/projects/$PROJECT/repository/tags?order_by=updated&sort=desc&per_page=100" \
            | jq -c '[.[] | {"ref": .commit.id, "tag": .name}]'
          exit 0
        fi

        echo "error: tag=true is only supported for github.com and gitlab.com" >&2
        exit 1
      fi

      # PR mode: check for open pull requests
      if [ "$PR" = "true" ]; then
        if [ -z "$TOKEN" ]; then
          echo "error: pr=true requires a token" >&2
          exit 1
        fi

        # GitHub PRs
        if echo "$URL" | grep -q "github.com"; then
          REPO=$(echo "$URL" | sed -E 's|https?://github\.com/||;s|\.git$||')
          curl -sf -H "Authorization: token $TOKEN" \
            "https://api.github.com/repos/$REPO/pulls?state=open&sort=updated&direction=desc&per_page=100" \
            | jq -c '[.[] | {"ref": .head.sha, "pr": (.number | tostring)}]'
          exit 0
        fi

        # GitLab MRs
        if echo "$URL" | grep -q "gitlab.com"; then
          PROJECT=$(echo "$URL" | sed -E 's|https?://gitlab\.com/||;s|\.git$||' | sed 's|/|%2F|g')
          curl -sf -H "PRIVATE-TOKEN: $TOKEN" \
            "https://gitlab.com/api/v4/projects/$PROJECT/merge_requests?state=opened&order_by=updated_at&sort=desc&per_page=100" \
            | jq -c '[.[] | {"ref": .sha, "pr": (.iid | tostring)}]'
          exit 0
        fi

        echo "error: pr=true is only supported for github.com and gitlab.com" >&2
        exit 1
      fi

      # GitHub API
      if [ -n "$TOKEN" ] && echo "$URL" | grep -q "github.com"; then
        REPO=$(echo "$URL" | sed -E 's|https?://github\.com/||;s|\.git$||')
        REF=$(curl -sf -H "Authorization: token $TOKEN" \
          "https://api.github.com/repos/$REPO/commits?sha=$BRANCH&per_page=1" \
          | jq -r '.[0].sha')
        if [ -n "$REF" ] && [ "$REF" != "null" ]; then
          echo "[{\"ref\":\"$REF\"}]"
          exit 0
        fi
      fi

      # GitLab API
      if [ -n "$TOKEN" ] && echo "$URL" | grep -q "gitlab.com"; then
        PROJECT=$(echo "$URL" | sed -E 's|https?://gitlab\.com/||;s|\.git$||' | sed 's|/|%2F|g')
        REF=$(curl -sf -H "PRIVATE-TOKEN: $TOKEN" \
          "https://gitlab.com/api/v4/projects/$PROJECT/repository/commits?ref_name=$BRANCH&per_page=1" \
          | jq -r '.[0].id')
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

      # Inject token into HTTPS URL if provided
      if [ -n "$TOKEN" ]; then
        URL=$(echo "$URL" | sed -E "s|https://|https://oauth2:$TOKEN@|")
      fi

      if [ "$TAG" = "true" ] && [ -n "$version_tag" ]; then
        # Tag mode: clone at the specific tag
        git clone -b "$version_tag" --depth 1 "$URL" "$param_name"
      elif [ "$PR" = "true" ] && [ -n "$version_pr" ]; then
        # PR mode: fetch the PR head ref
        git clone "$URL" "$param_name"
        cd "$param_name"
        git fetch origin "pull/$version_pr/head:pr-$version_pr"
        git checkout "pr-$version_pr"
      elif [ -n "$BRANCH" ]; then
        git clone -b "$BRANCH" "$URL" "$param_name"
        cd "$param_name"
        git checkout "$version_ref"
      else
        git clone "$URL" "$param_name"
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
