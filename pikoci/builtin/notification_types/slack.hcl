notification_type "slack" {
  params = [
    "webhook_url",
    "base_url",
  ]
  notify "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      WEBHOOK_URL="$param_webhook_url"
      BASE_URL="$param_base_url"
      BASE_URL="$${BASE_URL%/}"
      MESSAGE="$NOTIFY_MESSAGE"

      if [ -z "$MESSAGE" ]; then
        STATUS="$notify_build_status"
        ICON=""
        case "$STATUS" in
          success) ICON="✅" ;;
          failure) ICON="❌" ;;
          cancel)  ICON="⚠️" ;;
        esac
        if [ -n "$STATUS" ]; then
          MESSAGE="$ICON [$BUILD_PIPELINE_NAME/$BUILD_JOB_NAME] Build #$BUILD_NUMBER - $STATUS"
        else
          MESSAGE="[$BUILD_PIPELINE_NAME/$BUILD_JOB_NAME] Build #$BUILD_NUMBER"
        fi
        if [ -n "$BASE_URL" ]; then
          LINK="$${BASE_URL}/teams/$${BUILD_TEAM_NAME}/pipelines/$${BUILD_PIPELINE_NAME}/jobs/$${BUILD_JOB_NAME}/builds/$${BUILD_NUMBER}"
          MESSAGE="$MESSAGE - $LINK"
        fi
      fi

      if [ -z "$WEBHOOK_URL" ]; then
        echo "error: webhook_url param is required" >&2
        exit 1
      fi

      BODY=$(jq -n --arg text "$MESSAGE" '{text: $text}')

      curl -sf -o /dev/null -X POST \
        -H "Content-Type: application/json" \
        "$WEBHOOK_URL" \
        -d "$BODY"

      echo "Sent Slack notification"
      EOT
    ]
  }
}
