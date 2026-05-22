package migrate

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/lopezator/migrator"
	"github.com/xescugc/pikoci/pikoci/mysql"
	"github.com/xescugc/pikoci/pikoci/mysql/migrate/migrations"
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

func adaptSQL(sql, system string) string {
	switch system {
	case mysql.Mem, mysql.SQLite:
		sql = strings.ReplaceAll(sql, "SET sql_mode = 'NO_AUTO_VALUE_ON_ZERO';", "")
		sql = strings.ReplaceAll(sql, "id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,", "id INTEGER PRIMARY KEY,")
		// SQLite doesn't support CASCADE on DROP TABLE
		sql = strings.ReplaceAll(sql, "DROP TABLE IF EXISTS pipelines CASCADE;", "DROP TABLE IF EXISTS pipelines;")
	case mysql.PostgreSQL:
		sql = strings.ReplaceAll(sql, "SET sql_mode = 'NO_AUTO_VALUE_ON_ZERO';", "")
		sql = strings.ReplaceAll(sql, "id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,", "id SERIAL PRIMARY KEY,")
		sql = strings.ReplaceAll(sql, "INT UNSIGNED NOT NULL", "INTEGER NOT NULL")
		sql = strings.ReplaceAll(sql, "INT UNSIGNED", "INTEGER")
		// Replace backtick-quoted identifiers with double-quote-quoted ones
		sql = strings.ReplaceAll(sql, "`type`", `"type"`)
		sql = strings.ReplaceAll(sql, "`check`", `"check"`)
		// PostgreSQL doesn't support RENAME COLUMN with the same syntax in older versions,
		// but ALTER TABLE ... RENAME COLUMN is standard and works in PG 9.6+
	case mysql.MySQL:
		// MySQL/MariaDB doesn't support CASCADE on DROP TABLE,
		// but SET FOREIGN_KEY_CHECKS=0 achieves the same effect
		sql = strings.ReplaceAll(sql, "DROP TABLE IF EXISTS pipelines CASCADE;",
			"SET FOREIGN_KEY_CHECKS = 0; DROP TABLE IF EXISTS pipelines; SET FOREIGN_KEY_CHECKS = 1;")
	}
	return sql
}
