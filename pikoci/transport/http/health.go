package http

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

func health(db *sql.DB, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")

		if err := db.PingContext(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"db":      "disconnected",
				"error":   err.Error(),
				"version": version,
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"db":      "connected",
			"version": version,
		})
	}
}
