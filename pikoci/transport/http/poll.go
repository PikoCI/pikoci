package http

import (
	"encoding/json"
	"net/http"

	"github.com/pikoci/pikoci/pikoci"
)

func pollNextWork(s pikoci.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item, err := s.PollNextWork(r.Context())
		if err != nil {
			encodeResponse(PollNextWorkResponse{Err: err.Error()}, w)
			return
		}
		if item == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(PollNextWorkResponse{WorkItem: item})
	})
}
