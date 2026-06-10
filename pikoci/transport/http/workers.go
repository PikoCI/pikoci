package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/wkr"
)

type WorkerHeartbeatRequest struct {
	Name        string `json:"name"`
	Hostname    string `json:"hostname"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	GoVersion   string `json:"go_version"`
	Version     string `json:"version"`
	Concurrency int    `json:"concurrency"`
	StartedAt   string `json:"started_at"`
}
type WorkerHeartbeatResponse struct {
	Err string `json:"error,omitempty"`
}

func (r WorkerHeartbeatResponse) Error() string { return r.Err }

func workerHeartbeat(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			req WorkerHeartbeatRequest
			ctx = r.Context()
		)
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			encodeResponse(WorkerHeartbeatResponse{Err: err.Error()}, w)
			return
		}

		var startedAt time.Time
		if req.StartedAt != "" {
			startedAt, _ = time.Parse(time.RFC3339, req.StartedAt)
		}

		wk := wkr.Worker{
			Name:        req.Name,
			Hostname:    req.Hostname,
			OS:          req.OS,
			Arch:        req.Arch,
			GoVersion:   req.GoVersion,
			Version:     req.Version,
			Concurrency: req.Concurrency,
			StartedAt:   startedAt,
		}

		err = s.WorkerHeartbeat(ctx, wk)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(WorkerHeartbeatResponse{Err: errs}, w)
	}
}

type ListWorkersResponse struct {
	Workers []*wkr.Worker `json:"data,omitempty"`
	Err     string        `json:"error,omitempty"`
}

func (r ListWorkersResponse) Error() string { return r.Err }

func listWorkers(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		workers, err := s.ListWorkers(ctx)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(ListWorkersResponse{Workers: workers, Err: errs}, w)
	}
}

type WorkersHealthResponse struct {
	Healthy bool   `json:"healthy"`
	Err     string `json:"error,omitempty"`
}

func (r WorkersHealthResponse) Error() string { return r.Err }

func workersHealth(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		healthy, err := s.WorkersHealth(ctx)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(WorkersHealthResponse{Healthy: healthy, Err: errs}, w)
	}
}

type DeleteWorkerResponse struct {
	Err string `json:"error,omitempty"`
}

func (r DeleteWorkerResponse) Error() string { return r.Err }

func deleteWorker(s pikoci.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		vars := mux.Vars(r)
		name := vars["worker_name"]

		err := s.DeleteWorker(ctx, name)
		var errs string
		if err != nil {
			errs = err.Error()
		}
		encodeResponse(DeleteWorkerResponse{Err: errs}, w)
	}
}
