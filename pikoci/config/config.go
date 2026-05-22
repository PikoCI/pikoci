package config

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

	PubSubSystem string `mapstructure:"pubsub-system"`

	LogLevel string `mapstructure:"log-level"`

	TeamCanonical       string `mapstructure:"team-canonical"`
	PipelineDisplayName string `mapstructure:"pipeline-name"`
	PipelineConfig      string `mapstructure:"pipeline-config"`
	PipelineVars        string `mapstructure:"vars"`
}
