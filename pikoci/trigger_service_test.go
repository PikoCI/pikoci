package pikoci_test

import (
	"context"
	"testing"

	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestFireTriggerNotifications_PipelineNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(nil, assert.AnError)

	// Should not panic even when pipeline lookup fails.
	s.S.FireTriggerNotifications(ctx, "main", "my-pipeline", "git.repo", nil)
}

func TestFireTriggerNotifications_NilRaw_NoOp(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Pipeline has no Raw bytes — hooks are skipped without running any exec.
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		Jobs: []job.Job{{Name: "my-job"}},
	}, nil)

	// No panics, no unexpected calls.
	s.S.FireTriggerNotifications(ctx, "main", "my-pipeline", "git.repo", nil)
}

func TestFireTriggerNotifications_WithVersionMeta_NilRaw(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Version metadata is accepted but pipeline has no Raw — no exec.
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{}, nil)

	versionMeta := map[string]interface{}{"ref": "abc123", "build": 42}
	s.S.FireTriggerNotifications(ctx, "main", "my-pipeline", "git.repo", versionMeta)
}
