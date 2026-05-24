service_type "nats" {
  params = ["version", "port"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-nats"
      docker rm -f $NAME 2>/dev/null || true
      docker run -d --name $NAME \
        -p $param_port:4222 \
        nats:$param_version
    EOT
    ]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "nc -z localhost $param_port"]
    interval = "1s"
    timeout  = "60s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-nats"
      docker rm -f $NAME 2>/dev/null || true
    EOT
    ]
  }
}
