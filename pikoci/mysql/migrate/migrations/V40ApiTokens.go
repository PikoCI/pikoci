package migrations

var V40ApiTokens = Migration{
	Name: "ApiTokens",
	SQL: `CREATE TABLE IF NOT EXISTS api_tokens (
    id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    token_hash VARCHAR(64) NOT NULL,
    token_prefix VARCHAR(12) NOT NULL,
    user_id INT UNSIGNED NOT NULL,
    team_id INT UNSIGNED NULL,
    role VARCHAR(20) NULL,
    expires_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP NULL,

    CONSTRAINT uq__api_tokens__name__user_id UNIQUE (name, user_id),
    CONSTRAINT uq__api_tokens__token_hash UNIQUE (token_hash),

    CONSTRAINT fk__api_tokens__users FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk__api_tokens__teams FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE
)`,
}
