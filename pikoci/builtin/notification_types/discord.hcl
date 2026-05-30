notification_type "discord" {
  params = [
    "webhook_url",
  ]
  notify "exec" {
    path = "/bin/sh"
    args = [
      "-ec",
      <<-EOT
      WEBHOOK_URL="$param_webhook_url"
      MESSAGE="$NOTIFY_MESSAGE"

      if [ -z "$MESSAGE" ]; then
        MESSAGE="[$BUILD_PIPELINE_NAME/$BUILD_JOB_NAME] Build #$BUILD_NUMBER"
      fi

      if [ -z "$WEBHOOK_URL" ]; then
        echo "error: webhook_url param is required" >&2
        exit 1
      fi

      BODY=$(jq -n --arg content "$MESSAGE" '{content: $content}')

      curl -sf -o /dev/null -X POST \
        -H "Content-Type: application/json" \
        "$WEBHOOK_URL" \
        -d "$BODY"

      echo "Sent Discord notification"
      EOT
    ]
  }
}
