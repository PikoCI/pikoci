service_type "kafka" {
  params = ["version", "port"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-kafka"
      docker rm -f $NAME 2>/dev/null || true
      docker run -d --name $NAME \
        -p $param_port:9092 \
        -e KAFKA_NODE_ID=0 \
        -e KAFKA_PROCESS_ROLES=controller,broker \
        -e KAFKA_CONTROLLER_QUORUM_VOTERS=0@localhost:9093 \
        -e KAFKA_LISTENERS=PLAINTEXT://:9092,CONTROLLER://:9093 \
        -e KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://127.0.0.1:$param_port \
        -e KAFKA_CONTROLLER_LISTENER_NAMES=CONTROLLER \
        -e KAFKA_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT \
        -e CLUSTER_ID=MkU3OEVBNTcwNTJENDM2Qk \
        apache/kafka:$param_version
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
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-kafka"
      docker rm -f $NAME 2>/dev/null || true
    EOT
    ]
  }
}
