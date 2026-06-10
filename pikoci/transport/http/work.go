package http

import "github.com/pikoci/pikoci/pikoci/queue"

// PollNextWorkResponse is the JSON response for the poll-next-work endpoint.
type PollNextWorkResponse struct {
	WorkItem *queue.WorkItem `json:"work_item,omitempty"`
	Err      string          `json:"error,omitempty"`
}

func (r PollNextWorkResponse) Error() string { return r.Err }
