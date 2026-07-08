package migrations

var V46OAuthProviders = Migration{
	Name: "OAuthProviders",
	SQL: `CREATE TABLE oauth_providers (
	id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name VARCHAR(255) NOT NULL,
	canonical VARCHAR(255) NOT NULL UNIQUE,
	` + "`type`" + ` VARCHAR(20) NOT NULL,
	issuer_url TEXT NOT NULL DEFAULT '',
	auth_url TEXT NOT NULL DEFAULT '',
	token_url TEXT NOT NULL DEFAULT '',
	userinfo_url TEXT NOT NULL DEFAULT '',
	scopes TEXT NOT NULL DEFAULT 'openid email profile',
	client_id VARCHAR(255) NOT NULL,
	client_secret TEXT NOT NULL DEFAULT '',
	username_claim VARCHAR(255) NOT NULL DEFAULT 'email',
	enabled BOOLEAN NOT NULL DEFAULT TRUE,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE oauth_user_links (
	id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	user_id INT UNSIGNED NOT NULL,
	provider_id INT UNSIGNED NOT NULL,
	subject VARCHAR(255) NOT NULL,
	email VARCHAR(255) NOT NULL DEFAULT '',
	UNIQUE(provider_id, subject),
	UNIQUE(user_id, provider_id),
	FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
	FOREIGN KEY (provider_id) REFERENCES oauth_providers(id) ON DELETE CASCADE
);
CREATE TABLE auth_settings (
	id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	local_auth_enabled BOOLEAN NOT NULL DEFAULT TRUE
);
INSERT INTO auth_settings(local_auth_enabled) VALUES (TRUE);`,
}
