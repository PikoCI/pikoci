// Package queue defines the publish/subscribe abstractions used by the scheduler
// and worker to exchange job and resource-check messages. The Topic and
// Subscription interfaces mirror the gocloud.dev/pubsub API so that
// implementations can be swapped between in-memory, NATS, or other backends.
package queue

import (
	"context"

	"gocloud.dev/pubsub"
)

//go:generate go tool mockgen -destination=../mock/topic.go -mock_names=Topic=Topic -package mock github.com/xescugc/pikoci/pikoci/queue Topic
//go:generate go tool mockgen -destination=../mock/subscription.go -mock_names=Subscription=Subscription -package mock github.com/xescugc/pikoci/pikoci/queue Subscription

// Topic publishes messages to all its subscribers. The interface is modeled
// after gocloud.dev/pubsub.Topic so that concrete implementations can be
// used interchangeably.
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

// Subscription receives messages published to a topic. The interface is modeled
// after gocloud.dev/pubsub.Subscription.
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

// Body is the JSON-serializable payload carried inside every pub/sub message.
// Depending on the queue, different fields are populated to identify the target
// team, pipeline, job, resource, or build.
type Body struct {
	TeamCanonical     string `json:"team_canonical,omitempty"`
	PipelineCanonical string `json:"pipeline_canonical,omitempty"`
	JobName           string `json:"job_name,omitempty"`
	ResourceCanonical string `json:"resource_canonical,omitempty"`
	VersionID         uint32 `json:"version_id,omitempty"`
	BuildID           uint32 `json:"build_id,omitempty"`
	RetryBuildNumber  string `json:"retry_build_number,omitempty"` // parent build number for retry numbering
	RetryBuildID      uint32 `json:"retry_build_id,omitempty"`     // build ID to copy resource versions from
}
