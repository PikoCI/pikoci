package migrations

var V36RunnerOverride = Migration{
	Name: "RunnerOverride",
	SQL: `ALTER TABLE notification_types ADD COLUMN runner TEXT;
ALTER TABLE resource_types ADD COLUMN runner TEXT;
ALTER TABLE secret_types ADD COLUMN runner TEXT;`,
}
