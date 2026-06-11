// Package config defines the configuration for a standalone PikoCI worker
// process. Fields are populated from command-line flags or environment
// variables via mapstructure tags.
package config

// Config holds all configuration values needed to run a standalone PikoCI
// worker, including the server URL, authentication token, and concurrency
// settings.
type Config struct {
	PikoCIURL   string `mapstructure:"pikoci-url"`
	WorkerToken string `mapstructure:"worker-token"`

	Name          string `mapstructure:"name"`
	Tags          string `mapstructure:"tags"`
	ExclusiveTags bool   `mapstructure:"exclusive-tags"`
	Concurrency   int    `mapstructure:"concurrency"`
	DrainTimeout  string `mapstructure:"drain-timeout"`

	LogLevel string `mapstructure:"log-level"`
}
