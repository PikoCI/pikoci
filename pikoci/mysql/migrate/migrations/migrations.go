package migrations

// Migration defines a basic struct to write
// the migraions
type Migration struct {
	Name string
	SQL  string
}

// Migrations is a list of all the Migrations
// we have, is defined as an array so it fails
// in compilation time if some order is wrong
// if it where to have more than one person working
// on it
var Migrations = [20]Migration{
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
}
