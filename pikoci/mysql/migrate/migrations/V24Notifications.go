package migrations

// V24Notifications adds notification_types and notifications tables.
var V24Notifications = Migration{
	Name: "Notifications",
	SQL: `
		CREATE TABLE IF NOT EXISTS notification_types (
			id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255),
			source VARCHAR(255),
			notify TEXT,
			params TEXT,

			pipeline_id INT UNSIGNED NOT NULL,

			CONSTRAINT uq__notification_types__pipeline__name UNIQUE ( pipeline_id, name ),

			CONSTRAINT fk__notification_types__pipelines
				FOREIGN KEY (pipeline_id) REFERENCES pipelines (id)
				ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS notifications (
			id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			type VARCHAR(255),
			name VARCHAR(255),
			canonical VARCHAR(255),
			params TEXT,
			message TEXT,
			on_events TEXT,
			jobs TEXT,
			exclude_jobs TEXT,

			pipeline_id INT UNSIGNED NOT NULL,

			CONSTRAINT uq__notifications__pipeline__canonical UNIQUE ( pipeline_id, canonical ),

			CONSTRAINT fk__notifications__pipelines
				FOREIGN KEY (pipeline_id) REFERENCES pipelines (id)
				ON DELETE CASCADE
		);
	`,
}
