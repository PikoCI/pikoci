package pikoci_test

import (
	"context"
	"testing"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGenerateTeamWorkerToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.WithValue(context.TODO(), pikoci.ActorContextKey, "admin")

	s.Teams.EXPECT().UpdateWorkerTokenSalt(gomock.Any(), "main", gomock.Any()).Return(nil)

	token, err := s.P.GenerateTeamWorkerToken(ctx, "main")
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestGetTeamWorkerToken_Exists(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Teams.EXPECT().FindWorkerTokenSalt(gomock.Any(), "main").Return("test-salt", nil)

	token, err := s.P.GetTeamWorkerToken(ctx, "main")
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestGetTeamWorkerToken_NoToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Teams.EXPECT().FindWorkerTokenSalt(gomock.Any(), "main").Return("", nil)

	token, err := s.P.GetTeamWorkerToken(ctx, "main")
	require.NoError(t, err)
	assert.Empty(t, token)
}
