// Package config defines the server-side configuration for the PikoCI application.
// Fields are populated from command-line flags or environment variables via
// mapstructure tags.
package config

// Config holds all configuration values needed to run the PikoCI server,
// including database credentials, JWT settings, worker options, and pipeline
// defaults.
type Config struct {
	Port int `mapstructure:"port"`

	DBSystem string `mapstructure:"db-system"`

	JWTSecret string `mapstructure:"jwt-secret"`

	Users []string `mapstructure:"users"`

	// MySQL
	DBHost     string `mapstructure:"db-host"`
	DBPort     int    `mapstructure:"db-port"`
	DBUser     string `mapstructure:"db-user"`
	DBPassword string `mapstructure:"db-password"`
	DBName     string `mapstructure:"db-name"`

	RunWorker    bool   `mapstructure:"run-worker"`
	Concurrency  int    `mapstructure:"concurrency"`
	DrainTimeout string `mapstructure:"drain-timeout"`

	LogLevel string `mapstructure:"log-level"`

	ExternalURL string `mapstructure:"external-url"`

	TeamCanonical       string `mapstructure:"team-canonical"`
	PipelineDisplayName string `mapstructure:"pipeline-name"`
	PipelineConfig      string `mapstructure:"pipeline-config"`
	PipelineVars        string `mapstructure:"vars"`
}
