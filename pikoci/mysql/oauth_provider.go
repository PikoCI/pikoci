package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/oauthprovider"
)

type OAuthProviderRepository struct {
	querier sqlr.Querier
}

func NewOAuthProviderRepository(db sqlr.Querier) *OAuthProviderRepository {
	return &OAuthProviderRepository{
		querier: db,
	}
}

type dbProvider struct {
	ID            sql.NullInt64
	Name          sql.NullString
	Canonical     sql.NullString
	Type          sql.NullString
	IssuerURL     sql.NullString
	AuthURL       sql.NullString
	TokenURL      sql.NullString
	UserinfoURL   sql.NullString
	Scopes        sql.NullString
	ClientID      sql.NullString
	ClientSecret  sql.NullString
	UsernameClaim sql.NullString
	Enabled       sql.NullBool
	CreatedAt     sql.NullTime
}

func newDBProvider(p oauthprovider.Provider) dbProvider {
	// alwaysValid wraps a string as valid even when empty, preventing NULL
	// insertion on NOT NULL columns that use DEFAULT ('').
	alwaysValid := func(s string) sql.NullString {
		return sql.NullString{String: s, Valid: true}
	}
	return dbProvider{
		Name:          toNullString(p.Name),
		Canonical:     toNullString(p.Canonical),
		Type:          toNullString(p.Type),
		IssuerURL:     alwaysValid(p.IssuerURL),
		AuthURL:       alwaysValid(p.AuthURL),
		TokenURL:      alwaysValid(p.TokenURL),
		UserinfoURL:   alwaysValid(p.UserinfoURL),
		Scopes:        alwaysValid(p.Scopes),
		ClientID:      toNullString(p.ClientID),
		ClientSecret:  alwaysValid(p.ClientSecret),
		UsernameClaim: alwaysValid(p.UsernameClaim),
		Enabled:       toNullBool(p.Enabled),
	}
}

func (dbp *dbProvider) toDomainEntity() *oauthprovider.Provider {
	return &oauthprovider.Provider{
		ID:            uint32(dbp.ID.Int64),
		Name:          dbp.Name.String,
		Canonical:     dbp.Canonical.String,
		Type:          dbp.Type.String,
		IssuerURL:     dbp.IssuerURL.String,
		AuthURL:       dbp.AuthURL.String,
		TokenURL:      dbp.TokenURL.String,
		UserinfoURL:   dbp.UserinfoURL.String,
		Scopes:        dbp.Scopes.String,
		ClientID:      dbp.ClientID.String,
		ClientSecret:  dbp.ClientSecret.String,
		UsernameClaim: dbp.UsernameClaim.String,
		Enabled:       dbp.Enabled.Bool,
		CreatedAt:     dbp.CreatedAt.Time,
	}
}

type dbUserLink struct {
	ID         sql.NullInt64
	UserID     sql.NullInt64
	ProviderID sql.NullInt64
	Subject    sql.NullString
	Email      sql.NullString
}

func newDBUserLink(l oauthprovider.UserLink) dbUserLink {
	return dbUserLink{
		UserID:     toNullInt64(int(l.UserID)),
		ProviderID: toNullInt64(int(l.ProviderID)),
		Subject:    toNullString(l.Subject),
		Email:      sql.NullString{String: l.Email, Valid: true},
	}
}

func (dbl *dbUserLink) toDomainEntity() *oauthprovider.UserLink {
	return &oauthprovider.UserLink{
		ID:         uint32(dbl.ID.Int64),
		UserID:     uint32(dbl.UserID.Int64),
		ProviderID: uint32(dbl.ProviderID.Int64),
		Subject:    dbl.Subject.String,
		Email:      dbl.Email.String,
	}
}

type dbAuthSettings struct {
	ID               sql.NullInt64
	LocalAuthEnabled sql.NullBool
}

func newDBAuthSettings(s oauthprovider.AuthSettings) dbAuthSettings {
	return dbAuthSettings{
		LocalAuthEnabled: toNullBool(s.LocalAuthEnabled),
	}
}

func (dbs *dbAuthSettings) toDomainEntity() *oauthprovider.AuthSettings {
	return &oauthprovider.AuthSettings{
		ID:               uint32(dbs.ID.Int64),
		LocalAuthEnabled: dbs.LocalAuthEnabled.Bool,
	}
}

func (r *OAuthProviderRepository) CreateProvider(ctx context.Context, p oauthprovider.Provider) (uint32, error) {
	dbp := newDBProvider(p)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO oauth_providers(name, canonical, `+"`type`"+`, issuer_url, auth_url, token_url, userinfo_url, scopes, client_id, client_secret, username_claim, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, dbp.Name, dbp.Canonical, dbp.Type, dbp.IssuerURL, dbp.AuthURL, dbp.TokenURL, dbp.UserinfoURL, dbp.Scopes, dbp.ClientID, dbp.ClientSecret, dbp.UsernameClaim, dbp.Enabled)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *OAuthProviderRepository) FindProvider(ctx context.Context, id uint32) (*oauthprovider.Provider, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT op.id, op.name, op.canonical, op.`+"`type`"+`, op.issuer_url, op.auth_url, op.token_url, op.userinfo_url, op.scopes, op.client_id, op.client_secret, op.username_claim, op.enabled, op.created_at
		FROM oauth_providers AS op
		WHERE op.id = ?
	`, id)

	p, err := scanProvider(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Provider: %w", err)
	}

	return p, nil
}

func (r *OAuthProviderRepository) FindProviderByCanonical(ctx context.Context, canonical string) (*oauthprovider.Provider, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT op.id, op.name, op.canonical, op.`+"`type`"+`, op.issuer_url, op.auth_url, op.token_url, op.userinfo_url, op.scopes, op.client_id, op.client_secret, op.username_claim, op.enabled, op.created_at
		FROM oauth_providers AS op
		WHERE op.canonical = ?
	`, canonical)

	p, err := scanProvider(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Provider: %w", err)
	}

	return p, nil
}

func (r *OAuthProviderRepository) FilterProviders(ctx context.Context) ([]*oauthprovider.Provider, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT op.id, op.name, op.canonical, op.`+"`type`"+`, op.issuer_url, op.auth_url, op.token_url, op.userinfo_url, op.scopes, op.client_id, op.client_secret, op.username_claim, op.enabled, op.created_at
		FROM oauth_providers AS op
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to filter Providers: %w", err)
	}
	defer rows.Close()

	ps, err := scanProviders(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Provider: %w", err)
	}

	return ps, nil
}

func (r *OAuthProviderRepository) FilterEnabledProviders(ctx context.Context) ([]*oauthprovider.Provider, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT op.id, op.name, op.canonical, op.`+"`type`"+`, op.issuer_url, op.auth_url, op.token_url, op.userinfo_url, op.scopes, op.client_id, op.client_secret, op.username_claim, op.enabled, op.created_at
		FROM oauth_providers AS op
		WHERE op.enabled = true
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to filter enabled Providers: %w", err)
	}
	defer rows.Close()

	ps, err := scanProviders(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Provider: %w", err)
	}

	return ps, nil
}

func (r *OAuthProviderRepository) UpdateProvider(ctx context.Context, canonical string, p oauthprovider.Provider) error {
	dbp := newDBProvider(p)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE oauth_providers AS op
		SET name = ?, canonical = ?, `+"`type`"+` = ?, issuer_url = ?, auth_url = ?, token_url = ?, userinfo_url = ?, scopes = ?, client_id = ?, client_secret = ?, username_claim = ?, enabled = ?
		WHERE op.canonical = ?
	`, dbp.Name, dbp.Canonical, dbp.Type, dbp.IssuerURL, dbp.AuthURL, dbp.TokenURL, dbp.UserinfoURL, dbp.Scopes, dbp.ClientID, dbp.ClientSecret, dbp.UsernameClaim, dbp.Enabled, canonical)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update Provider: %w", err)
	}

	return nil
}

func (r *OAuthProviderRepository) DeleteProvider(ctx context.Context, canonical string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM oauth_providers AS op
		WHERE op.canonical = ?
	`, canonical)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the Provider: %w", err)
	}

	return nil
}

func (r *OAuthProviderRepository) CreateUserLink(ctx context.Context, link oauthprovider.UserLink) (uint32, error) {
	dbl := newDBUserLink(link)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO oauth_user_links(user_id, provider_id, subject, email)
		VALUES (?, ?, ?, ?)
	`, dbl.UserID, dbl.ProviderID, dbl.Subject, dbl.Email)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *OAuthProviderRepository) FindUserLink(ctx context.Context, providerID uint32, subject string) (*oauthprovider.UserLink, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT ol.id, ol.user_id, ol.provider_id, ol.subject, ol.email
		FROM oauth_user_links AS ol
		WHERE ol.provider_id = ? AND ol.subject = ?
	`, providerID, subject)

	l, err := scanUserLink(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan UserLink: %w", err)
	}

	return l, nil
}

func (r *OAuthProviderRepository) FindUserLinksByUser(ctx context.Context, userID uint32) ([]*oauthprovider.UserLink, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT ol.id, ol.user_id, ol.provider_id, ol.subject, ol.email
		FROM oauth_user_links AS ol
		WHERE ol.user_id = ?
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to filter UserLinks: %w", err)
	}
	defer rows.Close()

	ls, err := scanUserLinks(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan UserLink: %w", err)
	}

	return ls, nil
}

func (r *OAuthProviderRepository) DeleteUserLink(ctx context.Context, id uint32) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM oauth_user_links AS ol
		WHERE ol.id = ?
	`, id)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the UserLink: %w", err)
	}

	return nil
}

func (r *OAuthProviderRepository) DeleteUserLinkByUserAndProvider(ctx context.Context, userID uint32, providerID uint32) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM oauth_user_links AS ol
		WHERE ol.user_id = ? AND ol.provider_id = ?
	`, userID, providerID)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the UserLink: %w", err)
	}

	return nil
}

func (r *OAuthProviderRepository) GetAuthSettings(ctx context.Context) (*oauthprovider.AuthSettings, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT s.id, s.local_auth_enabled
		FROM auth_settings AS s
		LIMIT 1
	`)

	s, err := scanAuthSettings(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan AuthSettings: %w", err)
	}

	return s, nil
}

func (r *OAuthProviderRepository) UpdateAuthSettings(ctx context.Context, settings oauthprovider.AuthSettings) error {
	dbs := newDBAuthSettings(settings)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE auth_settings AS s
		SET local_auth_enabled = ?
		WHERE s.id = ?
	`, dbs.LocalAuthEnabled, settings.ID)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update AuthSettings: %w", err)
	}

	return nil
}

func scanProvider(s sqlr.Scanner) (*oauthprovider.Provider, error) {
	var p dbProvider

	err := s.Scan(
		&p.ID,
		&p.Name,
		&p.Canonical,
		&p.Type,
		&p.IssuerURL,
		&p.AuthURL,
		&p.TokenURL,
		&p.UserinfoURL,
		&p.Scopes,
		&p.ClientID,
		&p.ClientSecret,
		&p.UsernameClaim,
		&p.Enabled,
		&p.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return p.toDomainEntity(), nil
}

func scanProviders(rows *sql.Rows) ([]*oauthprovider.Provider, error) {
	var ps []*oauthprovider.Provider

	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan provider: %w", err)
		}
		ps = append(ps, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan provider: %w", err)
	}
	return ps, nil
}

func scanUserLink(s sqlr.Scanner) (*oauthprovider.UserLink, error) {
	var l dbUserLink

	err := s.Scan(
		&l.ID,
		&l.UserID,
		&l.ProviderID,
		&l.Subject,
		&l.Email,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return l.toDomainEntity(), nil
}

func scanUserLinks(rows *sql.Rows) ([]*oauthprovider.UserLink, error) {
	var ls []*oauthprovider.UserLink

	for rows.Next() {
		l, err := scanUserLink(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user link: %w", err)
		}
		ls = append(ls, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan user link: %w", err)
	}
	return ls, nil
}

func scanAuthSettings(s sqlr.Scanner) (*oauthprovider.AuthSettings, error) {
	var as dbAuthSettings

	err := s.Scan(
		&as.ID,
		&as.LocalAuthEnabled,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return as.toDomainEntity(), nil
}
