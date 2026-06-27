package http

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/auditlog"
)

// ListAuditLogResponse is the JSON response for the audit log list endpoint.
type ListAuditLogResponse struct {
	Entries []*auditlog.Entry `json:"data,omitempty"`
	Meta    *PageMeta         `json:"meta,omitempty"`
	Err     string            `json:"error,omitempty"`
}

func (r ListAuditLogResponse) Error() string { return r.Err }

func listAuditLog(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		vars := mux.Vars(r)
		tc := vars["team_canonical"]

		before, after, limit := parsePaginationParams(r)

		opts := auditlog.FilterOpts{
			Limit: limit,
		}
		if before != nil {
			opts.Before = before
		}
		if after != nil {
			opts.After = after
		}

		q := r.URL.Query()
		if v := q["user"]; len(v) > 0 {
			opts.Actors = v
		}
		if v := q["exclude_user"]; len(v) > 0 {
			opts.ExcludeActors = v
		}
		if v := q["action"]; len(v) > 0 {
			actions := make([]auditlog.Action, len(v))
			for i, a := range v {
				actions[i] = auditlog.Action(a)
			}
			opts.Actions = actions
		}
		if v := q["exclude_action"]; len(v) > 0 {
			actions := make([]auditlog.Action, len(v))
			for i, a := range v {
				actions[i] = auditlog.Action(a)
			}
			opts.ExcludeActions = actions
		}
		if v := q["pipeline"]; len(v) > 0 {
			opts.Pipelines = v
		}
		if v := q.Get("since"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				opts.Since = &t
			}
		}
		if v := q.Get("until"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				opts.Until = &t
			}
		}

		entries, hasMore, err := s.ListAuditLog(ctx, tc, opts)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		var meta *PageMeta
		if len(entries) > 0 {
			meta = &PageMeta{
				HasMore:  hasMore,
				OldestID: entries[len(entries)-1].ID,
				NewestID: entries[0].ID,
			}
		}
		encodeResponse(ListAuditLogResponse{Entries: entries, Meta: meta, Err: errs}, w)
	}
}
