package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteDBFile(t *testing.T) {
	touch := func(t *testing.T, path string) {
		t.Helper()
		require.NoError(t, os.WriteFile(path, nil, 0o600))
	}

	t.Run("UnsetUsesDefault", func(t *testing.T) {
		def := filepath.Join(t.TempDir(), "default.db")

		got, err := sqliteDBFile("", def)
		require.NoError(t, err)
		assert.Equal(t, def, got)
	})

	t.Run("AbsolutePathUsedAsIs", func(t *testing.T) {
		dir := t.TempDir()
		want := filepath.Join(dir, "explicit.db")

		got, err := sqliteDBFile(want, filepath.Join(dir, "default.db"))
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("RelativePathResolvesAgainstWorkingDir", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		wd, err := os.Getwd()
		require.NoError(t, err)

		got, err := sqliteDBFile("pikoci.db", filepath.Join(dir, "default.db"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(wd, "pikoci.db"), got)
	})

	t.Run("RefusesToAbandonExistingDefaultDatabase", func(t *testing.T) {
		dir := t.TempDir()
		def := filepath.Join(dir, "default.db")
		target := filepath.Join(dir, "explicit.db")
		touch(t, def)

		_, err := sqliteDBFile(target, def)
		require.Error(t, err)
		assert.Contains(t, err.Error(), target)
		assert.Contains(t, err.Error(), def)
		assert.NoFileExists(t, target)
	})

	t.Run("ExistingTargetWinsOverDefaultDatabase", func(t *testing.T) {
		dir := t.TempDir()
		def := filepath.Join(dir, "default.db")
		target := filepath.Join(dir, "explicit.db")
		touch(t, def)
		touch(t, target)

		got, err := sqliteDBFile(target, def)
		require.NoError(t, err)
		assert.Equal(t, target, got)
	})

	t.Run("NamingTheDefaultPathIsAllowed", func(t *testing.T) {
		def := filepath.Join(t.TempDir(), "default.db")
		touch(t, def)

		got, err := sqliteDBFile(def, def)
		require.NoError(t, err)
		assert.Equal(t, def, got)
	})

	t.Run("RejectsQuestionMarkInPath", func(t *testing.T) {
		dir := t.TempDir()

		_, err := sqliteDBFile(filepath.Join(dir, "a?b.db"), filepath.Join(dir, "default.db"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot contain")
	})
}
