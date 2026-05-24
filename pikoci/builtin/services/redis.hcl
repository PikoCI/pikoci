service_type "redis" {
  params = ["version", "port"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-redis"
      docker rm -f $NAME 2>/dev/null || true
      docker run -d --name $NAME \
        -p $param_port:6379 \
        redis:$param_version
    EOT
    ]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "redis-cli -p $param_port ping"]
    interval = "1s"
    timeout  = "30s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-redis"
      docker rm -f $NAME 2>/dev/null || true
    EOT
    ]
  }
}
