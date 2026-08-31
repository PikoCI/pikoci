package mysql

import (
	"context"
	"database/sql"
	"strings"

	"github.com/cycloidio/sqlr"
)

// PGQuerier wraps a querier and rewrites ? placeholders to $N for PostgreSQL.
type PGQuerier struct {
	db sqlr.Querier
}

// NewPGQuerier returns a PGQuerier wrapping the given querier. It takes the
// interface rather than *sql.DB so a transaction can be wrapped too: inside a
// unit of work the repositories are handed an *sql.Tx, which does no rewriting
// of its own.
func NewPGQuerier(db sqlr.Querier) *PGQuerier {
	return &PGQuerier{db: db}
}

func rewritePlaceholders(query string) string {
	var b strings.Builder
	n := 1
	inSingleQuote := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c == '\'' {
			inSingleQuote = !inSingleQuote
			b.WriteByte(c)
		} else if c == '?' && !inSingleQuote {
			b.WriteByte('$')
			b.WriteString(itoa(n))
			n++
		} else if c == '`' && !inSingleQuote {
			// Replace MySQL backtick quoting with PostgreSQL double-quote quoting
			b.WriteByte('"')
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func (p *PGQuerier) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return p.db.QueryRowContext(ctx, rewritePlaceholders(query), args...)
}

func (p *PGQuerier) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return p.db.QueryContext(ctx, rewritePlaceholders(query), args...)
}

func (p *PGQuerier) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	q := rewritePlaceholders(query)
	trimmed := strings.TrimSpace(strings.ToUpper(q))

	// PostgreSQL doesn't support LastInsertId. For INSERT statements,
	// append RETURNING id and use QueryRow to capture the generated ID.
	if strings.HasPrefix(trimmed, "INSERT") && !strings.Contains(trimmed, "RETURNING") {
		q = strings.TrimRight(q, "; \n\t") + " RETURNING id"
		var id int64
		err := p.db.QueryRowContext(ctx, q, args...).Scan(&id)
		if err != nil {
			return nil, err
		}
		return pgResult{id: id, rows: 1}, nil
	}

	return p.db.ExecContext(ctx, q, args...)
}

// pgResult implements sql.Result for PostgreSQL INSERT RETURNING.
type pgResult struct {
	id   int64
	rows int64
}

func (r pgResult) LastInsertId() (int64, error) { return r.id, nil }
func (r pgResult) RowsAffected() (int64, error) { return r.rows, nil }
