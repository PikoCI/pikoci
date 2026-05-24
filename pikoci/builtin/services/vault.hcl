service_type "vault" {
  params = ["version", "port", "root_token"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-vault"
      docker rm -f $NAME 2>/dev/null || true
      docker run -d --name $NAME \
        -p $param_port:8200 \
        -e VAULT_DEV_ROOT_TOKEN_ID=$param_root_token \
        -e VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200 \
        -e SKIP_SETCAP=1 \
        --cap-add IPC_LOCK \
        hashicorp/vault:$param_version
    EOT
    ]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "curl -sf http://127.0.0.1:$param_port/v1/sys/health"]
    interval = "2s"
    timeout  = "60s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-vault"
      docker rm -f $NAME 2>/dev/null || true
    EOT
    ]
  }
}
