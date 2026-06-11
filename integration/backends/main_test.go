//go:build integration

package backends_test

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func dbSystems() []string {
	v := envOr("PIKOCI_TEST_DB_SYSTEMS", "mem")
	return strings.Split(v, ",")
}

type dbSetup struct {
	db      *sql.DB
	querier sqlr.Querier
	system  string
}

func openDB(t *testing.T, system string) *dbSetup {
	t.Helper()

	var (
		host, user, password, dbName string
		port                         int
	)

	switch system {
	case "mem":
		// no config needed
	case "sqlite":
		// Use temp file
	case "mysql":
		hp := envOr("PIKOCI_TEST_MYSQL_HOST", "127.0.0.1:3306")
		parts := strings.Split(hp, ":")
		host = parts[0]
		port, _ = strconv.Atoi(parts[1])
		user = envOr("PIKOCI_TEST_MYSQL_USER", "root")
		password = envOr("PIKOCI_TEST_MYSQL_PASSWORD", "root123")
		dbName = fmt.Sprintf("qid_test_%s_%d", system, os.Getpid())
	case "postgresql":
		hp := envOr("PIKOCI_TEST_PG_HOST", "127.0.0.1:5432")
		parts := strings.Split(hp, ":")
		host = parts[0]
		port, _ = strconv.Atoi(parts[1])
		user = envOr("PIKOCI_TEST_PG_USER", "postgres")
		password = envOr("PIKOCI_TEST_PG_PASSWORD", "postgres123")
		dbName = fmt.Sprintf("qid_test_%s_%d", system, os.Getpid())
	default:
		t.Fatalf("unknown db system: %s", system)
	}

	opts := mysql.Options{
		DBName:          dbName,
		MultiStatements: true,
		ClientFoundRows: true,
		System:          system,
	}

	if system == "sqlite" {
		tmpFile := t.TempDir() + "/test.db"
		opts.DBFile = tmpFile
	}

	// Drop any leftover test database from a previous run
	if dbName != "" {
		switch system {
		case mysql.MySQL:
			dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?multiStatements=true", user, password, host, port)
			if cleanDB, err := sql.Open("mysql", dsn); err == nil {
				cleanDB.Exec("DROP DATABASE IF EXISTS " + dbName)
				cleanDB.Close()
			}
		case mysql.PostgreSQL:
			dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable", host, port, user, password)
			if cleanDB, err := sql.Open("postgres", dsn); err == nil {
				cleanDB.Exec(fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()", dbName))
				cleanDB.Exec("DROP DATABASE IF EXISTS " + dbName)
				cleanDB.Close()
			}
		}
	}

	db, err := mysql.New(host, port, user, password, opts)
	if err != nil {
		t.Fatalf("failed to open db for system %s: %v", system, err)
	}

	t.Cleanup(func() {
		if system != "mem" && system != "sqlite" {
			db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		}
		db.Close()
	})

	var querier sqlr.Querier = db
	if mysql.IsPostgreSQL(system) {
		querier = mysql.NewPGQuerier(db)
	}

	return &dbSetup{
		db:      db,
		querier: querier,
		system:  system,
	}
}

func migrateDB(t *testing.T, setup *dbSetup) {
	t.Helper()
	err := migrate.Migrate(setup.db, setup.system)
	if err != nil {
		t.Fatalf("migration failed for system %s: %v", setup.system, err)
	}
}
