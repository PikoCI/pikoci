package migrations

// V29SerialGroups creates the job_serial_groups join table for cross-job mutual exclusion.
var V29SerialGroups = Migration{
	Name: "SerialGroups",
	SQL: `CREATE TABLE job_serial_groups (
	job_id INTEGER NOT NULL,
	serial_group VARCHAR(255) NOT NULL,
	PRIMARY KEY (job_id, serial_group),
	FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);
CREATE INDEX idx_job_serial_groups_group ON job_serial_groups (serial_group);`,
}
