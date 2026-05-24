package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// exportTables defines the order in which tables are copied during export.
// The order respects FK constraints so that parent rows exist before children.
var exportTables = []string{
	"teams", "users", "teams_users", "pipelines", "jobs", "builds", "resources",
	"resource_versions", "resource_types", "runners", "secret_types", "secrets",
	"build_get_versions", "triggers", "schema_migrations",
}

// Export creates a SQLite file containing all data from srcDB.
// migrateFn is called on the destination DB to create the schema before copying data.
// It returns the path to the temporary file. The caller is responsible
// for removing the file when done.
func Export(ctx context.Context, srcDB *sql.DB, srcSystem string, migrateFn func(*sql.DB, string) error) (string, error) {
	tmpFile, err := os.CreateTemp("", "pikoci-export-*.db")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	dstDB, err := New("", 0, "", "", Options{
		System:          SQLite,
		DBFile:          tmpPath,
		MultiStatements: true,
	})
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to open destination SQLite: %w", err)
	}
	defer dstDB.Close()

	if err := migrateFn(dstDB, SQLite); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to run migrations on export db: %w", err)
	}

	for _, table := range exportTables {
		exists, err := tableExists(ctx, srcDB, srcSystem, table)
		if err != nil {
			os.Remove(tmpPath)
			return "", fmt.Errorf("failed to check table %q: %w", table, err)
		}
		if !exists {
			continue
		}
		if err := copyTable(ctx, srcDB, dstDB, srcSystem, table); err != nil {
			os.Remove(tmpPath)
			return "", fmt.Errorf("failed to copy table %q: %w", table, err)
		}
	}

	return tmpPath, nil
}

func copyTable(ctx context.Context, srcDB, dstDB *sql.DB, srcSystem, table string) error {
	selectQuery := fmt.Sprintf("SELECT * FROM %s", quoteIdentifier(table, srcSystem))

	rows, err := srcDB.QueryContext(ctx, selectQuery)
	if err != nil {
		return fmt.Errorf("failed to select: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to get columns: %w", err)
	}
	if len(cols) == 0 {
		return nil
	}

	tx, err := dstDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	quotedCols := make([]string, len(cols))
	for i, c := range cols {
		quotedCols[i] = "`" + c + "`"
	}
	placeholders := make([]string, len(cols))
	for i := range cols {
		placeholders[i] = "?"
	}

	// Clear any rows seeded by migrations to avoid unique constraint conflicts.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM `%s`", table)); err != nil {
		return fmt.Errorf("failed to clear table: %w", err)
	}

	insertSQL := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)",
		table,
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "),
	)

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert: %w", err)
	}
	defer stmt.Close()

	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		if _, err := stmt.ExecContext(ctx, vals...); err != nil {
			return fmt.Errorf("failed to insert row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("row iteration error: %w", err)
	}

	return tx.Commit()
}

// tableExists checks whether a table exists in the given database.
func tableExists(ctx context.Context, db *sql.DB, system, table string) (bool, error) {
	var query string
	if IsPostgreSQL(system) {
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = $1"
	} else if system == SQLite || system == Mem {
		query = "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
	} else {
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = ?"
	}
	var count int
	if err := db.QueryRowContext(ctx, query, table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// quoteIdentifier quotes a table name appropriately for the given DB system.
func quoteIdentifier(name, system string) string {
	if IsPostgreSQL(system) {
		return `"` + name + `"`
	}
	return "`" + name + "`"
}
