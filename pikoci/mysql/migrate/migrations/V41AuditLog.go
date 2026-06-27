package migrations

var V41AuditLog = Migration{
	Name: "AuditLog",
	SQL: `CREATE TABLE IF NOT EXISTS audit_log (
		id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		team_id INT UNSIGNED NOT NULL,
		actor VARCHAR(255) NOT NULL,
		action VARCHAR(50) NOT NULL,
		target_type VARCHAR(50) NOT NULL,
		target_name VARCHAR(255) NOT NULL,
		details JSON NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk__audit_log__teams FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE
	);
	CREATE INDEX idx__audit_log__team_id__created_at ON audit_log (team_id, created_at);
	CREATE INDEX idx__audit_log__team_id__actor ON audit_log (team_id, actor);
	CREATE INDEX idx__audit_log__team_id__action ON audit_log (team_id, action);`,
}
