package migrations

var V43BuildApprovals = Migration{
	Name: "BuildApprovals",
	SQL: `CREATE TABLE IF NOT EXISTS build_approvals (
		id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		build_id INT UNSIGNED NOT NULL,
		username VARCHAR(255) NOT NULL,
		action VARCHAR(20) NOT NULL,
		message TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk__build_approvals__builds FOREIGN KEY (build_id) REFERENCES builds(id) ON DELETE CASCADE,
		UNIQUE (build_id, username)
	);
	CREATE INDEX idx__build_approvals__build_id ON build_approvals (build_id);`,
}
