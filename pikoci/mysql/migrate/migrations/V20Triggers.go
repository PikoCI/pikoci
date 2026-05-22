package migrations

// V20Triggers creates the triggers table for cross-pipeline triggering.
var V20Triggers = Migration{
	Name: "Triggers",
	SQL: `
		CREATE TABLE IF NOT EXISTS triggers (
			id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			team_id INT UNSIGNED NOT NULL,
			name VARCHAR(255) NOT NULL,
			version TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

			CONSTRAINT fk__triggers__teams
				FOREIGN KEY (team_id) REFERENCES teams (id)
				ON DELETE CASCADE
		);

		CREATE INDEX idx_triggers_team_name ON triggers (team_id, name);
	`,
}
