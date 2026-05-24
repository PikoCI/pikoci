package http

import (
	"database/sql"
	"net/http"
	"os"
	"time"

	"github.com/xescugc/pikoci/pikoci/mysql"
	"github.com/xescugc/pikoci/pikoci/mysql/migrate"
)

func exportDatabase(db *sql.DB, dbSystem string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpPath, err := mysql.Export(r.Context(), db, dbSystem, func(db *sql.DB, system string) error {
			return migrate.Migrate(db, system)
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer os.Remove(tmpPath)

		f, err := os.Open(tmpPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer f.Close()

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="pikoci.db"`)
		http.ServeContent(w, r, "pikoci.db", time.Now(), f)
	}
}
