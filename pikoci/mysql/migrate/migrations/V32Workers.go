package migrations

var V32Workers = Migration{
	Name: "Workers",
	SQL: `CREATE TABLE workers (
  id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(255) NOT NULL UNIQUE,
  hostname VARCHAR(255) NOT NULL DEFAULT '',
  os VARCHAR(50) NOT NULL DEFAULT '',
  arch VARCHAR(50) NOT NULL DEFAULT '',
  go_version VARCHAR(50) NOT NULL DEFAULT '',
  version VARCHAR(50) NOT NULL DEFAULT '',
  concurrency INT NOT NULL DEFAULT 1,
  queues VARCHAR(255) NOT NULL DEFAULT '',
  started_at TIMESTAMP NOT NULL,
  last_ping_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_workers_last_ping_at ON workers (last_ping_at);`,
}
