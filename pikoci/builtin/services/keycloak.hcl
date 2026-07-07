service_type "keycloak" {
  params = ["version", "port", "admin_user", "admin_password", "realm_json"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-keycloak"
      docker rm -f $NAME 2>/dev/null || true
      docker run -d --name $NAME \
        -p $param_port:8080 \
        -e KC_BOOTSTRAP_ADMIN_USERNAME=$param_admin_user \
        -e KC_BOOTSTRAP_ADMIN_PASSWORD=$param_admin_password \
        quay.io/keycloak/keycloak:$param_version \
        start-dev --import-realm
      if [ -n "$param_realm_json" ] && [ -f "$param_realm_json" ]; then
        docker cp "$param_realm_json" $NAME:/opt/keycloak/data/import/realm.json
        docker restart $NAME
      fi
    EOT
    ]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "curl -sf http://127.0.0.1:$param_port/health/ready"]
    interval = "3s"
    timeout  = "120s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-keycloak"
      docker rm -f $NAME 2>/dev/null || true
    EOT
    ]
  }
}
