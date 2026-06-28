package migrations

var V44JobApproveColumns = Migration{
	Name: "JobApproveColumns",
	SQL: `ALTER TABLE jobs ADD COLUMN approve_label VARCHAR(255) NULL;
ALTER TABLE jobs ADD COLUMN approve_timeout VARCHAR(50) NULL;
ALTER TABLE jobs ADD COLUMN approve_count INT UNSIGNED NOT NULL DEFAULT 0;`,
}
