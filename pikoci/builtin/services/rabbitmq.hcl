service_type "rabbitmq" {
  params = ["version", "port"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-rabbit"
      docker rm -f $NAME 2>/dev/null || true
      docker run -d --name $NAME \
        -p $param_port:5672 \
        -e RABBITMQ_NODENAME=rabbit \
        rabbitmq:$param_version
    EOT
    ]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "nc -z localhost $param_port"]
    interval = "3s"
    timeout  = "60s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-rabbit"
      docker rm -f $NAME 2>/dev/null || true
    EOT
    ]
  }
}
