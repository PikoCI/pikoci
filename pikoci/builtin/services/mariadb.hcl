service_type "mariadb" {
  params = ["version", "port", "root_password"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-mariadb"
      docker rm -f $NAME 2>/dev/null || true
      docker run -d --name $NAME \
        -p $param_port:3306 \
        -e MYSQL_ROOT_PASSWORD=$param_root_password \
        mariadb:$param_version
    EOT
    ]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-mariadb"
      docker exec $NAME mariadb -uroot -p$${param_root_password} -e 'SELECT 1'
    EOT
    ]
    interval = "2s"
    timeout  = "60s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-mariadb"
      docker rm -f $NAME 2>/dev/null || true
    EOT
    ]
  }
}
