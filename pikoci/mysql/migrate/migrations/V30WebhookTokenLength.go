package migrations

// V30WebhookTokenLength widens the webhook_token column to accommodate the
// resource name prefix added to new tokens (format: name_uuid).
var V30WebhookTokenLength = Migration{
	Name: "WebhookTokenLength",
	SQL:  `ALTER TABLE resources MODIFY COLUMN webhook_token VARCHAR(255) DEFAULT '';`,
}
