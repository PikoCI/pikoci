package migrations

// V23BackfillWebhookTokens generates webhook tokens for resources that were
// created before V10 added the column. The hex(randomblob()) expression works
// on SQLite; adaptSQL in migrate.go replaces it for MySQL and PostgreSQL.
var V23BackfillWebhookTokens = Migration{
	Name: "BackfillWebhookTokens",
	SQL: `UPDATE resources SET webhook_token = lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6))) WHERE webhook_token = '' OR webhook_token IS NULL;`,
}
