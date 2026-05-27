// Package config defines the configuration for a standalone PikoCI worker
// process. Fields are populated from command-line flags or environment
// variables via mapstructure tags.
package config

// Config holds all configuration values needed to run a standalone PikoCI
// worker, including the server URL, authentication token, concurrency settings,
// and pub/sub configuration.
type Config struct {
	PikoCIURL   string `mapstructure:"pikoci-url"`
	WorkerToken string `mapstructure:"worker-token"`

	Concurrency  int    `mapstructure:"concurrency"`
	DrainTimeout string `mapstructure:"drain-timeout"`
	PubSubSystem string `mapstructure:"pubsub-system"`
	Queues       string `mapstructure:"queues"`

	LogLevel string `mapstructure:"log-level"`
}
