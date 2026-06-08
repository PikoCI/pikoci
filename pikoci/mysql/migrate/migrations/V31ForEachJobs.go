package migrations

// V31ForEachJobs adds columns to track for_each job instances. for_each_group
// stores the base job name before expansion, and for_each_key stores the
// specific key within the group.
var V31ForEachJobs = Migration{
	Name: "ForEachJobs",
	SQL: `ALTER TABLE jobs ADD COLUMN for_each_group VARCHAR(255) DEFAULT NULL;
ALTER TABLE jobs ADD COLUMN for_each_key VARCHAR(255) DEFAULT NULL;
CREATE INDEX idx_jobs_for_each_group ON jobs (pipeline_id, for_each_group);`,
}
