// Package migrate runs database schema migrations for PikoCI.
// It adapts SQL statements to the target database system (MySQL, PostgreSQL,
// SQLite, or in-memory SQLite) before applying them.
package migrate

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/lopezator/migrator"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate/migrations"
)

// Migrate runs the migrations on the provided db
func Migrate(db *sql.DB, system string, opts ...migrator.Option) error {
	ms := make([]interface{}, 0, len(migrations.Migrations))
	for _, m := range migrations.Migrations {
		val := m
		ms = append(ms, &migrator.Migration{
			Name: val.Name,
			Func: func(tx *sql.Tx) error {
				s := adaptSQL(val.SQL, system)
				if _, err := tx.Exec(s); err != nil {
					return err
				}
				return nil
			},
		})
	}

	allOpts := []migrator.Option{migrator.Migrations(ms...)}
	allOpts = append(allOpts, opts...)

	m, err := migrator.New(allOpts...)
	if err != nil {
		return fmt.Errorf("error while creating the migration: %w", err)
	}

	if err := m.Migrate(db); err != nil {
		return fmt.Errorf("error while migrating: %w", err)
	}

	return nil
}

// sqliteUUIDExpr is the SQLite expression used to generate UUID v4 values.
const sqliteUUIDExpr = `lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))`

// modifyColumnRe matches ALTER TABLE ... MODIFY COLUMN statements that SQLite
// does not support (SQLite doesn't enforce VARCHAR length constraints anyway).
var modifyColumnRe = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+\S+\s+MODIFY\s+COLUMN\s+[^;]+;?`)

// modifyColumnRePostgres captures table, column and type from MODIFY COLUMN
// so it can be rewritten to PostgreSQL's ALTER COLUMN ... TYPE syntax.
var modifyColumnRePostgres = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+(\S+)\s+MODIFY\s+COLUMN\s+(\S+)\s+(\S+)[^;]*;?`)

func replaceSQLiteUUID(sql, replacement string) string {
	return strings.ReplaceAll(sql, sqliteUUIDExpr, replacement)
}

func adaptSQL(sql, system string) string {
	switch system {
	case mysql.Mem, mysql.SQLite:
		sql = strings.ReplaceAll(sql, "SET sql_mode = 'NO_AUTO_VALUE_ON_ZERO';", "")
		sql = strings.ReplaceAll(sql, "id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,", "id INTEGER PRIMARY KEY,")
		// SQLite doesn't support CASCADE on DROP TABLE
		sql = strings.ReplaceAll(sql, "DROP TABLE IF EXISTS pipelines CASCADE;", "DROP TABLE IF EXISTS pipelines;")
		// SQLite doesn't support MODIFY COLUMN (and doesn't enforce VARCHAR length)
		sql = modifyColumnRe.ReplaceAllString(sql, "")
	case mysql.PostgreSQL:
		sql = strings.ReplaceAll(sql, "SET sql_mode = 'NO_AUTO_VALUE_ON_ZERO';", "")
		sql = strings.ReplaceAll(sql, "id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,", "id SERIAL PRIMARY KEY,")
		sql = strings.ReplaceAll(sql, "INT UNSIGNED NOT NULL", "INTEGER NOT NULL")
		sql = strings.ReplaceAll(sql, "INT UNSIGNED", "INTEGER")
		// PostgreSQL uses TIMESTAMP instead of DATETIME
		sql = strings.ReplaceAll(sql, "DATETIME", "TIMESTAMP")
		// Replace backtick-quoted identifiers with double-quote-quoted ones
		sql = strings.ReplaceAll(sql, "`type`", `"type"`)
		sql = strings.ReplaceAll(sql, "`check`", `"check"`)
		sql = strings.ReplaceAll(sql, "`commit`", `"commit"`)
		// PostgreSQL uses ALTER COLUMN ... TYPE instead of MODIFY COLUMN
		sql = modifyColumnRePostgres.ReplaceAllString(sql, "ALTER TABLE $1 ALTER COLUMN $2 TYPE $3;")
		// PostgreSQL doesn't support RENAME COLUMN with the same syntax in older versions,
		// but ALTER TABLE ... RENAME COLUMN is standard and works in PG 9.6+
		// Replace SQLite UUID expression with PostgreSQL's gen_random_uuid()
		sql = replaceSQLiteUUID(sql, "gen_random_uuid()")
	case mysql.MySQL:
		// MySQL/MariaDB doesn't support CASCADE on DROP TABLE,
		// but SET FOREIGN_KEY_CHECKS=0 achieves the same effect
		sql = strings.ReplaceAll(sql, "DROP TABLE IF EXISTS pipelines CASCADE;",
			"SET FOREIGN_KEY_CHECKS = 0; DROP TABLE IF EXISTS pipelines; SET FOREIGN_KEY_CHECKS = 1;")
		// Replace SQLite UUID expression with MySQL's UUID()
		sql = replaceSQLiteUUID(sql, "UUID()")
	}
	return sql
}
