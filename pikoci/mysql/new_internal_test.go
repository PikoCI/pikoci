package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMySQLDSN(t *testing.T) {
	dsn := mysqlDSN("db.local", 3306, "pikoci", "secret", "pikoci", Options{ClientFoundRows: true, MultiStatements: true})
	assert.Equal(t, "pikoci:secret@tcp(db.local:3306)/pikoci?clientFoundRows=true&parseTime=true&multiStatements=true", dsn)

	// parseTime does not depend on any option: without it every non-NULL
	// DATETIME (e.g. resources.next_check) fails to scan into time.Time.
	assert.Contains(t, mysqlDSN("h", 1, "u", "p", "", Options{}), "parseTime=true")
}
