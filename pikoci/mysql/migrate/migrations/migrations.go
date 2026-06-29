// Package migrations defines the ordered list of database schema migrations
// for PikoCI. Each migration has a name and raw SQL that gets adapted to the
// target database system at runtime by the migrate package.
package migrations

// Migration represents a single database schema migration with a name
// and the SQL statement to execute.
type Migration struct {
	// Name is the human-readable identifier for this migration.
	Name string
	// SQL is the raw SQL statement to execute, written in MySQL syntax.
	// It is adapted to other database systems at runtime.
	SQL string
}

// Migrations is the ordered list of all schema migrations. It is defined as
// a fixed-size array so that the compiler catches ordering conflicts when
// multiple developers add migrations concurrently.
var Migrations = [45]Migration{
	V0Initial,
	V1ResourceCheckInterval,
	V2JobsAndBuilds,
	V3Runners,
	V4Cron,
	V5Duration,
	V6ResourceVersion,
	V7InputsToParams,
	V8UsersAdnTeams,
	V9OrderedPlan,
	V10WebhookToken,
	V11PublicPipelines,
	V12Source,
	V13Scheduler,
	V14Secrets,
	V15SecretTypeConfig,
	V16BuildGetVersions,
	V17BuildNumber,
	V18Concurrency,
	V19OnCancel,
	V20Triggers,
	V21PipelineCanonical,
	V22PendingBuilds,
	V23BackfillWebhookTokens,
	V24Notifications,
	V25Cache,
	V26PausePipelineJob,
	V27ResourcePin,
	V28JobTimeout,
	V29SerialGroups,
	V30WebhookTokenLength,
	V31ForEachJobs,
	V32Workers,
	V33ClearQueuesColumn,
	V34WorkerTags,
	V35BaselineVersionID,
	V36RunnerOverride,
	V37WorkerCommit,
	V38RetrySourceBuildID,
	V39TeamMemberRoles,
	V40ApiTokens,
	V41AuditLog,
	V42RenameRoles,
	V43BuildApprovals,
	V44JobApproveColumns,
}
