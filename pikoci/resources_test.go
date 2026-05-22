package pikoci_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xescugc/pikoci/pikoci/queue"
	"github.com/xescugc/pikoci/pikoci/resource"
	"go.uber.org/mock/gomock"
	"gocloud.dev/pubsub"
)

func TestCreateResourceVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().CreateVersion(ctx, "main", "my-pipeline", "git.repo", gomock.Any()).Return(uint32(1), nil)

	v, err := s.S.CreateResourceVersion(ctx, "main", "my-pipeline", "git.repo", resource.Version{
		Version: map[string]interface{}{"ref": "abc123"},
	})
	require.NoError(t, err)
	assert.Equal(t, uint32(1), v.ID)
}

func TestCreateResourceVersion_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateResourceVersion(ctx, "INVALID", "my-pipeline", "git.repo", resource.Version{})
	require.Error(t, err)

	_, err = s.S.CreateResourceVersion(ctx, "main", "INVALID", "git.repo", resource.Version{})
	require.Error(t, err)

	_, err = s.S.CreateResourceVersion(ctx, "main", "my-pipeline", "INVALID", resource.Version{})
	require.Error(t, err)
}

func TestListResourceVersions(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo").Return([]*resource.Version{
		{ID: 1},
		{ID: 2},
	}, nil)

	vers, err := s.S.ListResourceVersions(ctx, "main", "my-pipeline", "git.repo")
	require.NoError(t, err)
	require.Len(t, vers, 2)
	// Should be reversed
	assert.Equal(t, uint32(2), vers[0].ID)
	assert.Equal(t, uint32(1), vers[1].ID)
}

func TestGetPipelineResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := &resource.Resource{ID: 1, Canonical: "git.repo"}
	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").Return(expected, nil)

	r, err := s.S.GetPipelineResource(ctx, "main", "my-pipeline", "git.repo")
	require.NoError(t, err)
	assert.Equal(t, expected, r)
}

func TestUpdatePipelineResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().Update(ctx, "main", "my-pipeline", "git.repo", gomock.Any()).Return(nil)

	err := s.S.UpdatePipelineResource(ctx, "main", "my-pipeline", "git.repo", resource.Resource{})
	require.NoError(t, err)
}

func TestTriggerPipelineResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").Return(&resource.Resource{
		ID: 1, Canonical: "git.repo",
	}, nil)

	expectedBody := queue.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "my-pipeline",
		ResourceCanonical: "git.repo",
	}
	mb, _ := json.Marshal(expectedBody)
	s.Topic.EXPECT().Send(ctx, &pubsub.Message{Body: mb}).Return(nil)

	// UpdatePipelineResource is called to set LastCheck
	s.Resources.EXPECT().Update(ctx, "main", "my-pipeline", "git.repo", gomock.Any()).Return(nil)

	err := s.S.TriggerPipelineResource(ctx, "main", "my-pipeline", "git.repo")
	require.NoError(t, err)
}
