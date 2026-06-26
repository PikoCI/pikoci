package mysql

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// lastInsertedID extracts the id from the query result.
// If the entity was not created, i.e. id == 0, the
// UnexpectedErrorExternalSystemDB error will be returned.
func lastInsertedID(res sql.Result) (uint32, error) {
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	if id == 0 {
		return 0, fmt.Errorf("the entity was not created")
	}

	return uint32(id), nil
}

// isEntityFound returns whether the SQL query did affect any rows.
func isEntityFound(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected SQL rows: %w", err)
	}

	if n == 0 {
		return fmt.Errorf("entity not found")
	}

	return nil
}

// toNullString returns sql.NullString. The string is considered valid if it's not empty.
func toNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// toNullBool returns sql.NullBool, that is always Valid
func toNullBool(b bool) sql.NullBool {
	return sql.NullBool{Bool: b, Valid: true}
}

// toNullInt64 returns sql.NullInt64. The int is considered valid if it's not equal 0.
func toNullInt64(i int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(i), Valid: i != 0}
}

// toNullTime returns sql.NullTIme. The time is considered valid if it's not equal Zero.
func toNullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: !t.IsZero()}
}

// toNullTimePtr returns sql.NullTime from a *time.Time pointer. Valid when non-nil.
func toNullTimePtr(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

// isUniqueViolation checks if an error is a unique constraint violation
// across SQLite, MySQL, and PostgreSQL.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || // SQLite
		strings.Contains(msg, "Duplicate entry") || // MySQL
		strings.Contains(msg, "duplicate key value") // PostgreSQL
}
