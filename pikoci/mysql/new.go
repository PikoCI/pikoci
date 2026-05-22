package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/VividCortex/mysqlerr"
	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"

	_ "modernc.org/sqlite"
)

const (
	Mem        = "mem"
	MySQL      = "mysql"
	SQLite     = "sqlite"
	PostgreSQL = "postgresql"
)

// New returns a new sql.DB with the provided parameters. If the Ping to the DB fails
// due to not existing DB it'll create the DB
// IsPostgreSQL returns true if the system is PostgreSQL.
func IsPostgreSQL(system string) bool {
	return system == PostgreSQL
}

func New(host string, port int, user, password string, ops Options) (*sql.DB, error) {
	switch ops.System {
	case MySQL:
		if host == "" {
			return nil, errors.New("host is a required parameter")
		} else if port == 0 {
			return nil, errors.New("port is a required parameter")
		} else if user == "" {
			return nil, errors.New("user is a required parameter")
		} else if password == "" {
			return nil, errors.New("password is a required parameter")
		}
	case PostgreSQL:
		if host == "" {
			return nil, errors.New("host is a required parameter")
		} else if port == 0 {
			return nil, errors.New("port is a required parameter")
		} else if user == "" {
			return nil, errors.New("user is a required parameter")
		} else if password == "" {
			return nil, errors.New("password is a required parameter")
		}
	case Mem:
	case SQLite:
		if ops.DBFile == "" {
			return nil, fmt.Errorf("DBFile is required")
		}
	default:
		return nil, fmt.Errorf("invalid db system %q", ops.System)
	}

	var (
		db  *sql.DB
		err error
	)

	switch ops.System {
	case Mem:
		// In-memory SQLite with shared cache (so multiple connections see the same data).
		// _pragma=foreign_keys(1) enables ON DELETE CASCADE on every connection in the pool
		// (per-connection PRAGMA wouldn't work with sql.DB's connection pool).
		// busy_timeout(5000) waits up to 5s before returning SQLITE_BUSY.
		// _txlock=immediate acquires the write lock at BEGIN instead of on first write.
		// Note: WAL is not supported on in-memory databases, only busy_timeout applies here.
		db, err = sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate")
	case SQLite:
		// File-backed SQLite. Data persists across restarts.
		// _pragma=foreign_keys(1) enables ON DELETE CASCADE on every connection in the pool.
		// journal_mode(WAL) allows concurrent reads during writes.
		// busy_timeout(5000) waits up to 5s before returning SQLITE_BUSY.
		// _txlock=immediate acquires the write lock at BEGIN instead of on first write,
		// ensuring busy_timeout properly retries instead of returning SQLITE_BUSY immediately.
		db, err = sql.Open("sqlite", ops.DBFile+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate")
	case PostgreSQL:
		// PostgreSQL has foreign keys enabled by default. sslmode=disable for local/dev setups.
		dsn := fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
			host, port, user, password, ops.DBName,
		)
		db, err = sql.Open("postgres", dsn)
	case MySQL:
		// MySQL/MariaDB. Foreign keys are enforced by default with InnoDB.
		// clientFoundRows: UPDATE returns rows matched instead of rows changed (needed for isEntityFound).
		// parseTime: scan DATE/DATETIME into time.Time.
		// multiStatements: allow multiple SQL statements in one Exec (needed for migrations).
		dsn := fmt.Sprintf(
			"%s:%s@tcp(%s:%d)/%s?clientFoundRows=%t&parseTime=%t&multiStatements=%t",
			user, password, host, port, ops.DBName, ops.ClientFoundRows, ops.ParseTime, ops.MultiStatements,
		)
		db, err = sql.Open("mysql", dsn)
	}
	if err != nil {
		return nil, fmt.Errorf("could not connect to the database: %w", err)
	}

	if err := db.Ping(); err != nil {
		if ops.System == MySQL {
			// If we get an error of ER_BAD_DB_ERROR means that the DB was not found, so not created
			// so we have to create it, which means to start a new connection without the DBName specified
			// and we create the DB and then "retry"
			var sqlerr *mysql.MySQLError
			if errors.As(err, &sqlerr) && sqlerr.Number == mysqlerr.ER_BAD_DB_ERROR {
				ndns := fmt.Sprintf(
					"%s:%s@tcp(%s:%d)/%s?clientFoundRows=%t&parseTime=%t&multiStatements=%t",
					user, password, host, port, "", ops.ClientFoundRows, ops.ParseTime, ops.MultiStatements,
				)

				ndb, err := sql.Open("mysql", ndns)
				if err != nil {
					return nil, fmt.Errorf("could not connect to the MySQL database to create database: %w", err)
				}
				defer ndb.Close()

				if err := ndb.Ping(); err != nil {
					return nil, fmt.Errorf("could not ping DB to create database: %w", err)
				}

				_, err = ndb.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", ops.DBName))
				if err != nil {
					return nil, fmt.Errorf("could not create DB %s: %w", ops.DBName, err)
				}

				if err := db.Ping(); err != nil {
					return nil, fmt.Errorf("could not ping DB to check database created: %w", err)
				}
			} else {
				return nil, fmt.Errorf("could not ping DB: %w", err)
			}
		} else if ops.System == PostgreSQL {
			// Auto-create database if it doesn't exist
			var pqerr *pq.Error
			if errors.As(err, &pqerr) && pqerr.Code == "3D000" {
				dsn := fmt.Sprintf(
					"host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable",
					host, port, user, password,
				)
				ndb, err := sql.Open("postgres", dsn)
				if err != nil {
					return nil, fmt.Errorf("could not connect to PostgreSQL to create database: %w", err)
				}
				defer ndb.Close()

				if err := ndb.Ping(); err != nil {
					return nil, fmt.Errorf("could not ping PostgreSQL to create database: %w", err)
				}

				// PostgreSQL doesn't have IF NOT EXISTS for CREATE DATABASE
				var exists bool
				err = ndb.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", ops.DBName).Scan(&exists)
				if err != nil {
					return nil, fmt.Errorf("could not check if database exists: %w", err)
				}
				if !exists {
					// Identifiers can't use parameterized queries
					_, err = ndb.Exec("CREATE DATABASE " + pqQuoteIdentifier(ops.DBName))
					if err != nil {
						return nil, fmt.Errorf("could not create DB %s: %w", ops.DBName, err)
					}
				}

				if err := db.Ping(); err != nil {
					return nil, fmt.Errorf("could not ping DB to check database created: %w", err)
				}
			} else {
				return nil, fmt.Errorf("could not ping DB: %w", err)
			}
		} else if ops.System != Mem && ops.System != SQLite {
			return nil, fmt.Errorf("could not ping DB: %w", err)
		}
	}

	return db, nil
}

// pqQuoteIdentifier quotes an identifier for safe use in PostgreSQL SQL statements.
func pqQuoteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// Options list of options that can be assigned to the New function
type Options struct {
	DBName            string
	ClientFoundRows   bool
	ParseTime         bool
	MultiStatements   bool
	System            string
	DBFile string
}
