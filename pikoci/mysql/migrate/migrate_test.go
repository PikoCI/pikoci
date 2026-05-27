package migrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/pikoci/pikoci/pikoci/mysql"
)

func TestAdaptSQL(t *testing.T) {
	input := `
		SET sql_mode = 'NO_AUTO_VALUE_ON_ZERO';
		CREATE TABLE IF NOT EXISTS users (
			id INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255),
			` + "`type`" + ` VARCHAR(255),
			` + "`check`" + ` TEXT,
			team_id INT UNSIGNED NOT NULL
		);
	`

	t.Run("sqlite", func(t *testing.T) {
		result := adaptSQL(input, mysql.SQLite)
		assert.NotContains(t, result, "SET sql_mode")
		assert.Contains(t, result, "id INTEGER PRIMARY KEY,")
		assert.NotContains(t, result, "AUTO_INCREMENT")
	})

	t.Run("mem", func(t *testing.T) {
		result := adaptSQL(input, mysql.Mem)
		assert.NotContains(t, result, "SET sql_mode")
		assert.Contains(t, result, "id INTEGER PRIMARY KEY,")
	})

	t.Run("postgresql", func(t *testing.T) {
		result := adaptSQL(input, mysql.PostgreSQL)
		assert.NotContains(t, result, "SET sql_mode")
		assert.Contains(t, result, "id SERIAL PRIMARY KEY,")
		assert.NotContains(t, result, "AUTO_INCREMENT")
		assert.Contains(t, result, "INTEGER NOT NULL")
		assert.NotContains(t, result, "INT UNSIGNED")
		assert.Contains(t, result, `"type"`)
		assert.Contains(t, result, `"check"`)
		assert.NotContains(t, result, "`type`")
		assert.NotContains(t, result, "`check`")
	})

	t.Run("mysql", func(t *testing.T) {
		result := adaptSQL(input, mysql.MySQL)
		assert.Contains(t, result, "SET sql_mode")
		assert.Contains(t, result, "AUTO_INCREMENT")
		assert.Contains(t, result, "INT UNSIGNED")
	})
}
