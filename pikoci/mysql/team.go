package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/team"
)

type TeamRepository struct {
	querier sqlr.Querier
}

func NewTeamRepository(db sqlr.Querier) *TeamRepository {
	return &TeamRepository{
		querier: db,
	}
}

type dbTeam struct {
	ID        sql.NullInt64
	Name      sql.NullString
	Canonical sql.NullString
}

type dbMember struct {
	Role sql.NullString
	User dbUser
}

func newDBTeam(u team.Team) dbTeam {
	return dbTeam{
		Name:      toNullString(u.Name),
		Canonical: toNullString(u.Canonical),
	}
}

func (dbt *dbTeam) toDomainEntity() *team.Team {
	return &team.Team{
		ID:        uint32(dbt.ID.Int64),
		Name:      dbt.Name.String,
		Canonical: dbt.Canonical.String,
	}
}

func (dbm *dbMember) toDomainEntity() *team.Member {
	return &team.Member{
		Role: role.Role(dbm.Role.String),
		User: *dbm.User.toDomainEntity(),
	}
}

func (r *TeamRepository) Create(ctx context.Context, t team.Team) (uint32, error) {
	dbt := newDBTeam(t)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO teams(name, canonical)
		VALUES (?, ?)
	`, dbt.Name, dbt.Canonical)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *TeamRepository) Update(ctx context.Context, tc string, t team.Team) error {
	dbt := newDBTeam(t)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE teams AS t
		SET name = ?, canonical = ?
		WHERE t.canonical = ?
	`, dbt.Name, dbt.Canonical, tc)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update team: %w", err)
	}

	return nil
}

func (r *TeamRepository) Find(ctx context.Context, tc string) (*team.WithMembers, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT t.id, t.name, t.canonical,
			tu.role, u.id, u.full_name, u.username, u.password, u.admin
		FROM teams AS t
		JOIN teams_users AS tu
			ON tu.team_id = t.id
		JOIN users AS u
			ON tu.user_id = u.id
		WHERE t.canonical = ?
	`, tc)
	if err != nil {
		return nil, fmt.Errorf("failed to filter Teams: %w", err)
	}

	t, err := scanTeamsWithMembers(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Team: %w", err)
	}

	if len(t) == 0 {
		return nil, fmt.Errorf("team not found: %s", tc)
	}

	return t[0], nil
}

func (r *TeamRepository) Filter(ctx context.Context, un string) ([]*team.WithMembers, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT t.id, t.name, t.canonical,
			tu.role, u.id, u.full_name, u.username, u.password, u.admin
		FROM teams AS t
		JOIN teams_users AS tu
			ON tu.team_id = t.id
		JOIN users AS u
			ON tu.user_id = u.id
		JOIN users AS ua
			ON ua.username = ?
		WHERE ua.admin OR t.id IN (
			SELECT t.id
			FROM teams AS t
			JOIN teams_users AS tu
				ON tu.team_id = t.id
			JOIN users AS u
				ON tu.user_id = u.id
			WHERE u.username = ?
		)
	`, un, un)
	if err != nil {
		return nil, fmt.Errorf("failed to filter Teams: %w", err)
	}

	ts, err := scanTeamsWithMembers(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Team: %w", err)
	}

	return ts, nil
}

func (r *TeamRepository) Delete(ctx context.Context, tc string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM teams AS t
		WHERE t.canonical = ?
	`, tc)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the Team: %w", err)
	}

	return nil
}

func (r *TeamRepository) CreateMember(ctx context.Context, tc string, tm team.Member) error {
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO teams_users(role, team_id, user_id)
		VALUES (?,
			(
				SELECT t.id
				FROM teams AS t
				WHERE t.canonical = ?
			),
			(
				SELECT u.id
				FROM users AS u
				WHERE u.username = ?
			)
		)
	`, string(tm.Role), tc, tm.User.Username)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	_, err = lastInsertedID(res)
	if err != nil {
		return fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return nil
}

func (r *TeamRepository) UpdateMember(ctx context.Context, tc, mc string, tm team.Member) error {
	res, err := r.querier.ExecContext(ctx, `
		UPDATE teams_users AS tu
		SET role = ?
		FROM (
			SELECT tu.id
			FROM teams_users AS tu
			JOIN teams AS t
				ON tu.team_id = t.id
			JOIN users AS u
				ON tu.user_id = u.id
			WHERE t.canonical = ? AND u.username = ?
		) AS ptu
		WHERE tu.id = ptu.id
	`, string(tm.Role), tc, mc)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update resource: %w", err)
	}

	return nil
}

func (r *TeamRepository) FindMember(ctx context.Context, tc, mc string) (*team.Member, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT tu.role, u.id, u.full_name, u.username, u.password, u.admin
		FROM teams_users AS tu
		JOIN teams AS t
			ON tu.team_id = t.id
		JOIN users AS u
			ON tu.user_id = u.id
		WHERE t.canonical = ? AND u.username = ?
	`, tc, mc)

	tm, err := scanTeamMember(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Team Member: %w", err)
	}

	return tm, nil
}

func (r *TeamRepository) DeleteMember(ctx context.Context, tc, mc string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM teams_users
		WHERE id IN (
			SELECT tu.id
			FROM teams_users AS tu
			JOIN teams AS t
				ON tu.team_id = t.id
			JOIN users AS u
				ON tu.user_id = u.id
			WHERE t.canonical = ? AND u.username = ?
		)
	`, tc, mc)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the Team Member: %w", err)
	}

	return nil
}

func (r *TeamRepository) FindWorkerTokenSalt(ctx context.Context, tc string) (string, error) {
	var salt sql.NullString
	err := r.querier.QueryRowContext(ctx, `
		SELECT worker_token_salt FROM teams WHERE canonical = ?
	`, tc).Scan(&salt)
	if err != nil {
		return "", fmt.Errorf("failed to find worker token salt: %w", err)
	}
	return salt.String, nil
}

func (r *TeamRepository) UpdateWorkerTokenSalt(ctx context.Context, tc, salt string) error {
	res, err := r.querier.ExecContext(ctx, `
		UPDATE teams SET worker_token_salt = ? WHERE canonical = ?
	`, salt, tc)
	if err != nil {
		return fmt.Errorf("failed to update worker token salt: %w", err)
	}
	return isEntityFound(res)
}

func scanTeamsWithMembers(rows *sql.Rows) ([]*team.WithMembers, error) {
	var ts []*team.WithMembers

	for rows.Next() {
		var (
			dbt dbTeam
			dbm dbMember
		)
		err := rows.Scan(
			&dbt.ID,
			&dbt.Name,
			&dbt.Canonical,
			&dbm.Role,
			&dbm.User.ID,
			&dbm.User.FullName,
			&dbm.User.Username,
			&dbm.User.Password,
			&dbm.User.Admin,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, fmt.Errorf("not found")
			}
			return nil, fmt.Errorf("failed to scan: %w", err)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to scan team: %w", err)
		}
		var found bool
		for _, t := range ts {
			nt := dbt.toDomainEntity()
			if t.ID == nt.ID {
				found = true
				t.Members = append(t.Members, *dbm.toDomainEntity())
				break
			}
		}
		if !found {
			ts = append(ts, &team.WithMembers{
				Team: *dbt.toDomainEntity(),
				Members: []team.Member{
					*dbm.toDomainEntity(),
				},
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan team: %w", err)
	}
	return ts, nil
}

func scanTeamMember(s sqlr.Scanner) (*team.Member, error) {
	var dbm dbMember

	err := s.Scan(
		&dbm.Role,
		&dbm.User.ID,
		&dbm.User.FullName,
		&dbm.User.Username,
		&dbm.User.Password,
		&dbm.User.Admin,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return dbm.toDomainEntity(), nil
}
