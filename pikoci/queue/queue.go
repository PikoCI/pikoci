package queue

import (
	"context"

	"gocloud.dev/pubsub"
)

//go:generate go tool mockgen -destination=../mock/topic.go -mock_names=Topic=Topic -package mock github.com/pikoci/pikoci/pikoci/queue Topic
//go:generate go tool mockgen -destination=../mock/subscription.go -mock_names=Subscription=Subscription -package mock github.com/pikoci/pikoci/pikoci/queue Subscription

// COPIED from https://pkg.go.dev/gocloud.dev/pubsub@v0.43.0#Topic as it's an interface
// and not a specific type
// Topic publishes messages to all its subscribers.
type Topic interface {
	// Send publishes a message. It only returns after the message has been sent, or failed to be sent. Send can be called from multiple goroutines at once.
	Send(ctx context.Context, m *pubsub.Message) (err error)
	// ErrorAs converts err to driver-specific types
	ErrorAs(err error, i any) bool
	// As converts i to driver-specific types.
	As(i any) bool

	// Shutdown flushes pending message sends and disconnects the Topic. It only returns after all pending messages have been sent.
	Shutdown(ctx context.Context) (err error)
}

type Subscription interface {
	// As converts i to driver-specific types
	As(i any) bool

	// ErrorAs converts err to driver-specific types.
	ErrorAs(err error, i any) bool

	// Receive receives and returns the next message from the Subscription's queue, blocking and polling if none are available. It can be called concurrently from multiple goroutines.
	Receive(ctx context.Context) (_ *pubsub.Message, err error)

	// Shutdown flushes pending ack sends and disconnects the Subscription.
	Shutdown(ctx context.Context) (err error)
}

type Body struct {
	TeamCanonical     string `json:"team_canonical,omitempty"`
	PipelineName      string `json:"pipeline_name,omitempty"`
	JobName           string `json:"job_name,omitempty"`
	ResourceCanonical string `json:"resource_canonical,omitempty"`
	VersionID         uint32 `json:"version_id,omitempty"`
	RetryBuildNumber  string `json:"retry_build_number,omitempty"` // parent build number for retry numbering
	RetryBuildID      uint32 `json:"retry_build_id,omitempty"`     // build ID to copy resource versions from
}
