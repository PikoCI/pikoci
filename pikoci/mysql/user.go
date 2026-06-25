package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/user"
)

type UserRepository struct {
	querier sqlr.Querier
}

func NewUserRepository(db sqlr.Querier) *UserRepository {
	return &UserRepository{
		querier: db,
	}
}

type dbUser struct {
	ID       sql.NullInt64
	FullName sql.NullString
	Username sql.NullString
	Password sql.NullString
	Admin    sql.NullBool
}

func newDBUser(u user.User) dbUser {
	return dbUser{
		FullName: toNullString(u.FullName),
		Username: toNullString(u.Username),
		Password: toNullString(u.Password),
		Admin:    toNullBool(u.Admin),
	}
}

func (dbu *dbUser) toDomainEntity() *user.User {
	return &user.User{
		ID:       uint32(dbu.ID.Int64),
		FullName: dbu.FullName.String,
		Username: dbu.Username.String,
		Password: dbu.Password.String,
		Admin:    dbu.Admin.Bool,
	}
}

func (r *UserRepository) Create(ctx context.Context, u user.User) (uint32, error) {
	dbu := newDBUser(u)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO users(full_name, username, password, admin)
		VALUES (?, ?, ?, ?)
	`, dbu.FullName, dbu.Username, dbu.Password, dbu.Admin)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *UserRepository) Update(ctx context.Context, un string, u user.User) error {
	dbu := newDBUser(u)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE users AS u
		SET full_name = ?, username = ?, password = ?, admin = ?
		WHERE u.username = ?
	`, dbu.FullName, dbu.Username, dbu.Password, dbu.Admin, un)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

func (r *UserRepository) Find(ctx context.Context, un string) (*user.User, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT u.id, u.full_name, u.username, u.password, u.admin
		FROM users AS u
		WHERE u.username = ?
	`, un)

	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan User: %w", err)
	}

	return u, nil
}

func (r *UserRepository) FindWithMemberships(ctx context.Context, un string) (*user.WithMemberships, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT u.id, u.full_name, u.username, u.password, u.admin,
			tu.role, t.id, t.name, t.canonical
		FROM users AS u
		LEFT JOIN teams_users AS tu
			ON tu.user_id = u.id
		LEFT JOIN teams AS t
			ON tu.team_id = t.id
		WHERE u.username = ?
	`, un)
	if err != nil {
		return nil, fmt.Errorf("failed to query user with memberships: %w", err)
	}

	u, err := scanUserWithMemberships(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan User: %w", err)
	}

	return u, nil
}

func (r *UserRepository) Filter(ctx context.Context) ([]*user.User, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT u.id, u.full_name, u.username, u.password, u.admin
		FROM users AS u
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to filter Users: %w", err)
	}

	us, err := scanUsers(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan User: %w", err)
	}

	return us, nil
}

func (r *UserRepository) Delete(ctx context.Context, un string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM users AS u
		WHERE u.username = ?
	`, un)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the User: %w", err)
	}

	return nil
}

func scanUser(s sqlr.Scanner) (*user.User, error) {
	var u dbUser

	err := s.Scan(
		&u.ID,
		&u.FullName,
		&u.Username,
		&u.Password,
		&u.Admin,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return u.toDomainEntity(), nil
}

func scanUsers(rows *sql.Rows) ([]*user.User, error) {
	var us []*user.User

	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		us = append(us, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan user: %w", err)
	}
	return us, nil
}

func scanUserWithMemberships(rows *sql.Rows) (*user.WithMemberships, error) {
	var um user.WithMemberships

	for rows.Next() {
		var (
			du       dbUser
			dt       dbTeam
			roleStr  sql.NullString
		)
		err := rows.Scan(
			&du.ID,
			&du.FullName,
			&du.Username,
			&du.Password,
			&du.Admin,
			&roleStr,
			&dt.ID,
			&dt.Name,
			&dt.Canonical,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				return nil, fmt.Errorf("not found")
			}
			return nil, fmt.Errorf("failed to scan: %w", err)
		}

		u := du.toDomainEntity()
		t := dt.toDomainEntity()
		um.User = *u

		if um.Memberships == nil {
			um.Memberships = make([]user.Member, 0)
		}
		if roleStr.Valid && t.Canonical != "" {
			um.Memberships = append(um.Memberships, user.Member{
				Role:          role.Role(roleStr.String),
				TeamCanonical: t.Canonical,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan user: %w", err)
	}
	return &um, nil
}
