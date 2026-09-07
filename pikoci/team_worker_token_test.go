package pikoci_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/golang-jwt/jwt/v5"
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

func TestGenerateTeamWorkerToken_UpdateSaltError(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.WithValue(context.TODO(), pikoci.ActorContextKey, "admin")

	s.Teams.EXPECT().UpdateWorkerTokenSalt(gomock.Any(), "main", gomock.Any()).
		Return(fmt.Errorf("db connection lost"))

	token, err := s.P.GenerateTeamWorkerToken(ctx, "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to store worker token salt")
	assert.Empty(t, token)
}

func TestGetTeamWorkerToken_FindSaltError(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Teams.EXPECT().FindWorkerTokenSalt(gomock.Any(), "main").
		Return("", fmt.Errorf("team not found"))

	token, err := s.P.GetTeamWorkerToken(ctx, "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find worker token salt")
	assert.Empty(t, token)
}

// TestGenerateTeamWorkerToken_RoundTrip verifies the JWT produced by
// GenerateTeamWorkerToken has the correct claims structure that
// validateWorkerToken expects: is_from_worker, team_canonical, salt.
func TestGenerateTeamWorkerToken_RoundTrip(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.WithValue(context.TODO(), pikoci.ActorContextKey, "admin")

	var storedSalt string
	s.Teams.EXPECT().UpdateWorkerTokenSalt(gomock.Any(), "main", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, salt string) error {
			storedSalt = salt
			return nil
		})

	token, err := s.P.GenerateTeamWorkerToken(ctx, "main")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.NotEmpty(t, storedSalt)

	// Parse the JWT and verify claims match what validateWorkerToken expects
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		return s.P.JWTSecret, nil
	})
	require.NoError(t, err)

	claims, ok := parsed.Claims.(jwt.MapClaims)
	require.True(t, ok)

	isWorker, _ := claims["is_from_worker"].(bool)
	assert.True(t, isWorker, "must have is_from_worker claim")

	tc, _ := claims["team_canonical"].(string)
	assert.Equal(t, "main", tc, "must have team_canonical claim")

	salt, _ := claims["salt"].(string)
	assert.Equal(t, storedSalt, salt, "salt claim must match stored salt")
}

func TestVerifyTeamWorkerTokenSalt_Valid(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Teams.EXPECT().FindWorkerTokenSalt(gomock.Any(), "main").Return("current-salt", nil)

	valid, err := s.P.VerifyTeamWorkerTokenSalt(ctx, "main", "current-salt")
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestVerifyTeamWorkerTokenSalt_Stale(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Teams.EXPECT().FindWorkerTokenSalt(gomock.Any(), "main").Return("current-salt", nil)

	valid, err := s.P.VerifyTeamWorkerTokenSalt(ctx, "main", "stale-salt")
	require.NoError(t, err)
	assert.False(t, valid, "a rotated salt must invalidate the old token")
}

func TestVerifyTeamWorkerTokenSalt_NoTokenGenerated(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Teams.EXPECT().FindWorkerTokenSalt(gomock.Any(), "main").Return("", nil)

	valid, err := s.P.VerifyTeamWorkerTokenSalt(ctx, "main", "any-salt")
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestVerifyTeamWorkerTokenSalt_LookupError(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Teams.EXPECT().FindWorkerTokenSalt(gomock.Any(), "main").Return("", fmt.Errorf("db connection lost"))

	_, err := s.P.VerifyTeamWorkerTokenSalt(ctx, "main", "any-salt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find worker token salt")
}
