package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteDBFile(t *testing.T) {
	write := func(t *testing.T, path, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	t.Run("AbsolutePathUsedAsIs", func(t *testing.T) {
		dir := t.TempDir()
		want := filepath.Join(dir, "explicit.db")

		got, warning, err := sqliteDBFile(want, filepath.Join(dir, "default.db"))
		require.NoError(t, err)
		assert.Equal(t, want, got)
		assert.Empty(t, warning)
	})

	t.Run("RelativePathResolvesAgainstWorkingDir", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		wd, err := os.Getwd()
		require.NoError(t, err)

		got, _, err := sqliteDBFile("pikoci.db", filepath.Join(dir, "default.db"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(wd, "pikoci.db"), got)
	})

	t.Run("FallsBackToExistingLegacyDatabase", func(t *testing.T) {
		dir := t.TempDir()
		legacy := filepath.Join(dir, "default.db")
		target := filepath.Join(dir, "explicit.db")
		write(t, legacy, "")

		got, warning, err := sqliteDBFile(target, legacy)
		require.NoError(t, err)
		assert.Equal(t, legacy, got)
		assert.Contains(t, warning, target)
		assert.Contains(t, warning, legacy)
		assert.NoFileExists(t, target)
	})

	t.Run("ExistingTargetWinsOverLegacyDatabase", func(t *testing.T) {
		dir := t.TempDir()
		legacy := filepath.Join(dir, "default.db")
		target := filepath.Join(dir, "explicit.db")
		write(t, legacy, "")
		write(t, target, "")

		got, warning, err := sqliteDBFile(target, legacy)
		require.NoError(t, err)
		assert.Equal(t, target, got)
		assert.Empty(t, warning)
	})

	t.Run("NamingTheLegacyPathIsAllowed", func(t *testing.T) {
		legacy := filepath.Join(t.TempDir(), "default.db")
		write(t, legacy, "")

		got, _, err := sqliteDBFile(legacy, legacy)
		require.NoError(t, err)
		assert.Equal(t, legacy, got)
	})

	t.Run("RefusesWhenOnlyTheMainFileWasMoved", func(t *testing.T) {
		dir := t.TempDir()
		legacy := filepath.Join(dir, "default.db")
		target := filepath.Join(dir, "explicit.db")
		write(t, target, "")
		write(t, legacy+"-wal", "pending writes")

		_, _, err := sqliteDBFile(target, legacy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), legacy+"-wal")
	})

	t.Run("EmptyLeftoverWalIsIgnored", func(t *testing.T) {
		dir := t.TempDir()
		legacy := filepath.Join(dir, "default.db")
		target := filepath.Join(dir, "explicit.db")
		write(t, target, "")
		write(t, legacy+"-wal", "")

		got, _, err := sqliteDBFile(target, legacy)
		require.NoError(t, err)
		assert.Equal(t, target, got)
	})

	t.Run("StatErrorOnLegacyIsReturned", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		dir := t.TempDir()
		locked := filepath.Join(dir, "locked")
		require.NoError(t, os.Mkdir(locked, 0o700))
		write(t, filepath.Join(locked, "default.db"), "")
		require.NoError(t, os.Chmod(locked, 0o000))
		t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

		_, _, err := sqliteDBFile(filepath.Join(dir, "explicit.db"), filepath.Join(locked, "default.db"))
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrPermission)
	})

	t.Run("MissingParentDirectoryIsNamed", func(t *testing.T) {
		dir := t.TempDir()
		missing := filepath.Join(dir, "nope")

		_, _, err := sqliteDBFile(filepath.Join(missing, "pikoci.db"), filepath.Join(dir, "default.db"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), missing)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("RejectsQuestionMarkInPath", func(t *testing.T) {
		dir := t.TempDir()

		_, _, err := sqliteDBFile(filepath.Join(dir, "a?b.db"), filepath.Join(dir, "default.db"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot contain")
	})
}
