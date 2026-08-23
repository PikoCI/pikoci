package migrations

// V53SecretStore adds the encrypted secret store: the server's wrapped age
// identity plus team- and pipeline-scoped secret values.
//
// Team and pipeline secrets live in separate tables rather than one table with
// a nullable pipeline_id, because a NULL pipeline_id cannot participate in a
// unique constraint (SQL treats NULLs as distinct), so team-level name
// uniqueness would not be enforceable at the database layer.
var V53SecretStore = Migration{
	Name: "SecretStore",
	SQL: `CREATE TABLE IF NOT EXISTS server_keys (
    id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    wrapped TEXT NOT NULL,
    recipient VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq__server_keys__name UNIQUE (name)
);

CREATE TABLE IF NOT EXISTS team_secrets (
    id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    canonical VARCHAR(255) NOT NULL,
    value TEXT NOT NULL,
    team_id INT UNSIGNED NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq__team_secrets__team_id__canonical UNIQUE (team_id, canonical),

    CONSTRAINT fk__team_secrets__teams FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS pipeline_secrets (
    id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    canonical VARCHAR(255) NOT NULL,
    value TEXT NOT NULL,
    pipeline_id INT UNSIGNED NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq__pipeline_secrets__pipeline_id__canonical UNIQUE (pipeline_id, canonical),

    CONSTRAINT fk__pipeline_secrets__pipelines FOREIGN KEY (pipeline_id) REFERENCES pipelines (id) ON DELETE CASCADE
)`,
}
