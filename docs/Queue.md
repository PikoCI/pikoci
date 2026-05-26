# Queue Backends

PikoCI uses a pub/sub queue to dispatch work from the server to workers. Configure with `--pubsub-system`.

PikoCI uses two separate queues: `pikoci-jobs` for job execution and `pikoci-checks` for resource checks. This ensures long-running jobs never block resource version detection.

## mem (default)

In-memory queue. Only works when the worker runs embedded in the server process (`--run-worker=true`). Suitable for development and single-process deployments.

```bash
pikoci server --jwt-secret my-secret --pubsub-system mem --run-worker
```

## nats

[NATS](https://nats.io/) messaging system. Set the server URL via the `NATS_SERVER_URL` environment variable.

```bash
export NATS_SERVER_URL="nats://localhost:4222"

pikoci server --jwt-secret my-secret --pubsub-system nats --run-worker=false
pikoci worker --pikoci-url http://localhost:8080 --pubsub-system nats --worker-token <token>
```

## rabbit

[RabbitMQ](https://www.rabbitmq.com/) message broker. Set the server URL via the `RABBIT_SERVER_URL` environment variable.

```bash
export RABBIT_SERVER_URL="amqp://guest:guest@localhost:5672/"

pikoci server --jwt-secret my-secret --pubsub-system rabbit --run-worker=false
pikoci worker --pikoci-url http://localhost:8080 --pubsub-system rabbit --worker-token <token>
```

## kafka

[Apache Kafka](https://kafka.apache.org/). Set the broker list via the `KAFKA_BROKERS` environment variable.

```bash
export KAFKA_BROKERS="localhost:9092"

pikoci server --jwt-secret my-secret --pubsub-system kafka --run-worker=false
pikoci worker --pikoci-url http://localhost:8080 --pubsub-system kafka --worker-token <token>
```

## Planned

AWS SQS and GCP Pub/Sub support is planned. See [#209](https://github.com/xescugc/pikoci/issues/209).

## Choosing a backend

| Backend | Use case |
|---------|----------|
| `mem` | Development, single-process deployments |
| `nats` | Lightweight production setups |
| `rabbit` | Existing RabbitMQ infrastructure |
| `kafka` | High-throughput, existing Kafka infrastructure |

When running workers separately, you must use a non-memory queue backend. See [Running Workers Separately](Workers).
