package http

import "github.com/pikoci/pikoci/pikoci/workitem"

// PollNextWorkResponse is the JSON response for the poll-next-work endpoint.
type PollNextWorkResponse struct {
	WorkItem *workitem.Item `json:"work_item,omitempty"`
	Err      string          `json:"error,omitempty"`
}

// Error returns the error message string, satisfying the Errorer interface.
func (r PollNextWorkResponse) Error() string { return r.Err }
