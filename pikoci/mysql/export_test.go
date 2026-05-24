package mysql_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xescugc/pikoci/pikoci/mysql"
	"github.com/xescugc/pikoci/pikoci/mysql/migrate"
)

func newMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := mysql.New("", 0, "", "", mysql.Options{
		System:          mysql.Mem,
		MultiStatements: true,
		ClientFoundRows: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func newSQLiteDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := mysql.New("", 0, "", "", mysql.Options{
		System:          mysql.SQLite,
		DBFile:          path,
		MultiStatements: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func runMigrations(t *testing.T, db *sql.DB, system string) {
	t.Helper()
	err := migrate.Migrate(db, system)
	require.NoError(t, err)
}

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		table    string
		system   string
		expected string
	}{
		{"mysql", "users", mysql.MySQL, "`users`"},
		{"sqlite", "users", mysql.SQLite, "`users`"},
		{"mem", "users", mysql.Mem, "`users`"},
		{"postgresql", "users", mysql.PostgreSQL, `"users"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, mysql.QuoteIdentifier(tt.table, tt.system))
		})
	}
}

func TestTableExists(t *testing.T) {
	ctx := context.Background()

	db := newMemDB(t)
	runMigrations(t, db, mysql.Mem)

	exists, err := mysql.TableExists(ctx, db, mysql.Mem, "users")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = mysql.TableExists(ctx, db, mysql.Mem, "nonexistent_table")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestTableExists_SQLite(t *testing.T) {
	ctx := context.Background()

	dbFile := t.TempDir() + "/test.db"
	db := newSQLiteDB(t, dbFile)
	runMigrations(t, db, mysql.SQLite)

	exists, err := mysql.TableExists(ctx, db, mysql.SQLite, "users")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = mysql.TableExists(ctx, db, mysql.SQLite, "nonexistent_table")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCopyTable(t *testing.T) {
	ctx := context.Background()

	srcDB := newMemDB(t)
	runMigrations(t, srcDB, mysql.Mem)

	// Insert test data
	_, err := srcDB.ExecContext(ctx, "INSERT INTO `teams` (`id`, `name`, `canonical`) VALUES (99, 'test-team', 'test-team')")
	require.NoError(t, err)

	dstFile := t.TempDir() + "/dst.db"
	dstDB := newSQLiteDB(t, dstFile)
	runMigrations(t, dstDB, mysql.SQLite)

	err = mysql.CopyTable(ctx, srcDB, dstDB, mysql.Mem, "teams")
	require.NoError(t, err)

	var name string
	err = dstDB.QueryRow("SELECT `name` FROM `teams` WHERE `id` = 99").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "test-team", name)
}

func TestCopyTable_ClearsMigrationSeededData(t *testing.T) {
	ctx := context.Background()

	srcDB := newMemDB(t)
	runMigrations(t, srcDB, mysql.Mem)

	dstFile := t.TempDir() + "/dst.db"
	dstDB := newSQLiteDB(t, dstFile)
	runMigrations(t, dstDB, mysql.SQLite)

	// Both have the seeded "main" team from migrations.
	// copyTable should clear the destination before copying.
	err := mysql.CopyTable(ctx, srcDB, dstDB, mysql.Mem, "teams")
	require.NoError(t, err)

	var count int
	err = dstDB.QueryRow("SELECT COUNT(*) FROM `teams`").Scan(&count)
	require.NoError(t, err)

	var srcCount int
	err = srcDB.QueryRow("SELECT COUNT(*) FROM `teams`").Scan(&srcCount)
	require.NoError(t, err)
	assert.Equal(t, srcCount, count)
}

func TestExport(t *testing.T) {
	ctx := context.Background()

	srcDB := newMemDB(t)
	runMigrations(t, srcDB, mysql.Mem)

	exportPath, err := mysql.Export(ctx, srcDB, mysql.Mem, func(db *sql.DB, system string) error {
		return migrate.Migrate(db, system)
	})
	require.NoError(t, err)
	defer os.Remove(exportPath)

	info, err := os.Stat(exportPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))

	exportDB := newSQLiteDB(t, exportPath)

	var count int
	err = exportDB.QueryRow("SELECT COUNT(*) FROM `teams`").Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 1)
}

func TestExport_SkipsMissingTables(t *testing.T) {
	ctx := context.Background()

	// Use a file-based SQLite to avoid shared in-memory state with other tests
	srcFile := t.TempDir() + "/src.db"
	srcDB := newSQLiteDB(t, srcFile)
	// Only create a minimal schema — not all tables from exportTables
	_, err := srcDB.Exec("CREATE TABLE teams (id INTEGER PRIMARY KEY, name TEXT, canonical TEXT)")
	require.NoError(t, err)
	_, err = srcDB.Exec("INSERT INTO teams (id, name, canonical) VALUES (1, 'main', 'main')")
	require.NoError(t, err)

	exportPath, err := mysql.Export(ctx, srcDB, mysql.SQLite, func(db *sql.DB, system string) error {
		return migrate.Migrate(db, system)
	})
	require.NoError(t, err)
	defer os.Remove(exportPath)

	exportDB := newSQLiteDB(t, exportPath)

	var name string
	err = exportDB.QueryRow("SELECT `name` FROM `teams` WHERE `canonical` = 'main'").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "main", name)
}

func TestExport_MigrationError(t *testing.T) {
	ctx := context.Background()

	srcDB := newMemDB(t)

	_, err := mysql.Export(ctx, srcDB, mysql.Mem, func(db *sql.DB, system string) error {
		return assert.AnError
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to run migrations")
}
