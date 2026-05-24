package mysql

import (
	"context"
	"database/sql"
)

// Exported wrappers for internal functions, used only by tests.
var QuoteIdentifier = quoteIdentifier

func TableExists(ctx context.Context, db *sql.DB, system, table string) (bool, error) {
	return tableExists(ctx, db, system, table)
}

func CopyTable(ctx context.Context, srcDB, dstDB *sql.DB, srcSystem, table string) error {
	return copyTable(ctx, srcDB, dstDB, srcSystem, table)
}
