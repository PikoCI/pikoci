package pikoci_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/apitoken"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/user"
	"go.uber.org/mock/gomock"
)

func TestCreateApiToken_Personal(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "admin").Return(&user.User{ID: 1, Username: "admin"}, nil)
	s.ApiTokens.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).Return(uint32(1), nil)

	token, err := s.S.CreateApiToken(ctx, "admin", "my-token", true, "", "", nil)
	require.NoError(t, err)
	require.NotNil(t, token)
	assert.Equal(t, "my-token", token.Name)
	assert.True(t, token.Personal)
	assert.Contains(t, token.Plaintext, "pko_")
	assert.Len(t, token.Plaintext, 68) // pko_ + 64 hex chars
	assert.Len(t, token.TokenPrefix, 12) // pko_ + 8 hex chars
}

func TestCreateApiToken_TeamScoped(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "admin").Return(&user.User{ID: 1, Username: "admin"}, nil)
	s.Users.EXPECT().FindWithMemberships(ctx, "admin").Return(&user.WithMemberships{
		User:        user.User{ID: 1, Username: "admin"},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}, nil)
	s.ApiTokens.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).Return(uint32(2), nil)

	token, err := s.S.CreateApiToken(ctx, "admin", "team-token", false, "main", role.Write, nil)
	require.NoError(t, err)
	require.NotNil(t, token)
	assert.Equal(t, "team-token", token.Name)
	assert.False(t, token.Personal)
	assert.Equal(t, "main", token.TeamCanonical)
	assert.Equal(t, role.Write, token.Role)
}

func TestCreateApiToken_EmptyName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateApiToken(ctx, "admin", "", true, "", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token name is required")
}

func TestCreateApiToken_NameTooLong(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	longName := make([]byte, 256)
	for i := range longName {
		longName[i] = 'a'
	}

	_, err := s.S.CreateApiToken(ctx, "admin", string(longName), true, "", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "255 characters")
}

func TestCreateApiToken_ExpiredDate(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "admin").Return(&user.User{ID: 1, Username: "admin"}, nil)

	pastDate := time.Now().Add(-24 * time.Hour)
	_, err := s.S.CreateApiToken(ctx, "admin", "expired", true, "", "", &pastDate)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expiration time must be in the future")
}

func TestCreateApiToken_WithExpiration(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "admin").Return(&user.User{ID: 1, Username: "admin"}, nil)
	s.ApiTokens.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).Return(uint32(1), nil)

	futureDate := time.Now().Add(24 * time.Hour)
	token, err := s.S.CreateApiToken(ctx, "admin", "expiring", true, "", "", &futureDate)
	require.NoError(t, err)
	require.NotNil(t, token.ExpiresAt)
	assert.True(t, token.ExpiresAt.After(time.Now()))
}

func TestCreateApiToken_TeamScoped_MissingTeam(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "admin").Return(&user.User{ID: 1, Username: "admin"}, nil)

	_, err := s.S.CreateApiToken(ctx, "admin", "bad", false, "", role.Read, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "team canonical is required")
}

func TestCreateApiToken_TeamScoped_InvalidRole(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "admin").Return(&user.User{ID: 1, Username: "admin"}, nil)

	_, err := s.S.CreateApiToken(ctx, "admin", "bad", false, "main", role.Public, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid role")
}

func TestCreateApiToken_TeamScoped_InsufficientRole(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "viewer-user").Return(&user.User{ID: 2, Username: "viewer-user"}, nil)
	s.Users.EXPECT().FindWithMemberships(ctx, "viewer-user").Return(&user.WithMemberships{
		User:        user.User{ID: 2, Username: "viewer-user"},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Read}},
	}, nil)

	_, err := s.S.CreateApiToken(ctx, "viewer-user", "bad", false, "main", role.Admin, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not have admin role")
}

func TestCreateApiToken_TeamScoped_RoleAtExactLevel(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "op-user").Return(&user.User{ID: 3, Username: "op-user"}, nil)
	s.Users.EXPECT().FindWithMemberships(ctx, "op-user").Return(&user.WithMemberships{
		User:        user.User{ID: 3, Username: "op-user"},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Write}},
	}, nil)
	s.ApiTokens.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).Return(uint32(1), nil)

	// Can create token at same level as user's role
	token, err := s.S.CreateApiToken(ctx, "op-user", "ok", false, "main", role.Write, nil)
	require.NoError(t, err)
	assert.Equal(t, role.Write, token.Role)
}

func TestCreateApiToken_TeamScoped_RoleBelowUserLevel(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "admin-user").Return(&user.User{ID: 4, Username: "admin-user"}, nil)
	s.Users.EXPECT().FindWithMemberships(ctx, "admin-user").Return(&user.WithMemberships{
		User:        user.User{ID: 4, Username: "admin-user"},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}, nil)
	s.ApiTokens.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).Return(uint32(1), nil)

	// Can create token below user's role
	token, err := s.S.CreateApiToken(ctx, "admin-user", "low", false, "main", role.Read, nil)
	require.NoError(t, err)
	assert.Equal(t, role.Read, token.Role)
}

func TestCreateApiToken_DuplicateName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "admin").Return(&user.User{ID: 1, Username: "admin"}, nil)
	s.ApiTokens.EXPECT().Create(ctx, gomock.Any(), gomock.Any()).Return(uint32(0), fmt.Errorf(`you already have a token named "my-token"`))

	_, err := s.S.CreateApiToken(ctx, "admin", "my-token", true, "", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "my-token")
}

func TestListApiTokens(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := []*apitoken.Token{
		{ID: 1, Name: "tok1", Personal: true},
		{ID: 2, Name: "tok2", Personal: false, TeamCanonical: "main", Role: role.Read},
	}
	s.ApiTokens.EXPECT().Filter(ctx, "admin").Return(expected, nil)

	tokens, err := s.S.ListApiTokens(ctx, "admin")
	require.NoError(t, err)
	assert.Len(t, tokens, 2)
}

func TestDeleteApiToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.ApiTokens.EXPECT().Delete(ctx, "admin", uint32(42)).Return(nil)

	err := s.S.DeleteApiToken(ctx, "admin", 42)
	require.NoError(t, err)
}

func TestDeleteApiToken_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.ApiTokens.EXPECT().Delete(ctx, "admin", uint32(999)).Return(fmt.Errorf("entity not found"))

	err := s.S.DeleteApiToken(ctx, "admin", 999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFindApiTokenByHash(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := &apitoken.AuthResult{
		Username:  "admin",
		UserID:    1,
		UserAdmin: true,
		Personal:  true,
		TokenID:   1,
	}
	s.ApiTokens.EXPECT().FindByHash(ctx, "somehash").Return(expected, nil)

	result, err := s.S.FindApiTokenByHash(ctx, "somehash")
	require.NoError(t, err)
	assert.Equal(t, "admin", result.Username)
	assert.True(t, result.Personal)
}

func TestFindApiTokenByHash_InvalidToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.ApiTokens.EXPECT().FindByHash(ctx, "badhash").Return(nil, fmt.Errorf("invalid API token"))

	_, err := s.S.FindApiTokenByHash(ctx, "badhash")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid API token")
}

func TestUpdateApiTokenLastUsed(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.ApiTokens.EXPECT().UpdateLastUsed(ctx, uint32(1)).Return(nil)
	s.S.UpdateApiTokenLastUsed(ctx, 1)
}

func TestUpdateApiTokenLastUsed_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Error should be logged, not returned (fire-and-forget)
	s.ApiTokens.EXPECT().UpdateLastUsed(ctx, uint32(99)).Return(fmt.Errorf("db error"))
	s.S.UpdateApiTokenLastUsed(ctx, 99)
}

// TestDeleteTeamMember_CleansUpTokens verifies the full flow:
// user has team-scoped token → removed from team → tokens for that team are deleted
// → token no longer authenticates.
func TestDeleteTeamMember_CleansUpTokens(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Step 1: Verify user's team-scoped token works before removal
	s.ApiTokens.EXPECT().FindByHash(ctx, "hash-before").Return(&apitoken.AuthResult{
		Username:      "pepito",
		UserID:        2,
		Personal:      false,
		TeamCanonical: "main",
		TokenRole:     role.Read,
		TokenID:       10,
	}, nil)
	result, err := s.S.FindApiTokenByHash(ctx, "hash-before")
	require.NoError(t, err)
	assert.Equal(t, "pepito", result.Username)
	assert.Equal(t, "main", result.TeamCanonical)

	// Step 2: Remove user from team — expect token cleanup
	s.Teams.EXPECT().Find(ctx, "main").Return(&team.WithMembers{
		Team: team.Team{ID: 1, Canonical: "main"},
		Members: []team.Member{
			{Role: role.Admin, User: user.User{Username: "admin"}},
			{Role: role.Read, User: user.User{Username: "pepito"}},
		},
	}, nil)
	s.Teams.EXPECT().DeleteMember(ctx, "main", "pepito").Return(nil)
	s.ApiTokens.EXPECT().DeleteByTeamMember(ctx, "pepito", "main").Return(nil)

	err = s.S.DeleteTeamMember(ctx, "main", "pepito")
	require.NoError(t, err)

	// Step 3: After removal, the token hash should no longer resolve
	s.ApiTokens.EXPECT().FindByHash(ctx, "hash-before").Return(nil, fmt.Errorf("invalid API token"))
	_, err = s.S.FindApiTokenByHash(ctx, "hash-before")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid API token")
}

// TestCreateApiToken_TeamScoped_WrongTeam verifies that a user cannot create
// a token for a team they are not a member of.
func TestCreateApiToken_TeamScoped_WrongTeam(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "pepito").Return(&user.User{ID: 2, Username: "pepito"}, nil)
	s.Users.EXPECT().FindWithMemberships(ctx, "pepito").Return(&user.WithMemberships{
		User:        user.User{ID: 2, Username: "pepito"},
		Memberships: []user.Member{{TeamCanonical: "team-a", Role: role.Admin}},
	}, nil)

	// Try to create a token for team-b — user is only a member of team-a
	_, err := s.S.CreateApiToken(ctx, "pepito", "bad-token", false, "team-b", role.Read, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not have read role on team \"team-b\"")
}

// TestCreateApiToken_TeamScoped_UserNotMember verifies that a user with no
// memberships at all cannot create a team-scoped token.
func TestCreateApiToken_TeamScoped_UserNotMember(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "lonely").Return(&user.User{ID: 3, Username: "lonely"}, nil)
	s.Users.EXPECT().FindWithMemberships(ctx, "lonely").Return(&user.WithMemberships{
		User:        user.User{ID: 3, Username: "lonely"},
		Memberships: []user.Member{},
	}, nil)

	_, err := s.S.CreateApiToken(ctx, "lonely", "no-access", false, "main", role.Read, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not have read role")
}

// TestDeleteTeamMember_PersonalTokensSurvive verifies that personal tokens are
// NOT deleted when a user is removed from a team (only team-scoped tokens are).
func TestDeleteTeamMember_PersonalTokensSurvive(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Remove pepito from main
	s.Teams.EXPECT().Find(ctx, "main").Return(&team.WithMembers{
		Team: team.Team{ID: 1, Canonical: "main"},
		Members: []team.Member{
			{Role: role.Admin, User: user.User{Username: "admin"}},
			{Role: role.Read, User: user.User{Username: "pepito"}},
		},
	}, nil)
	s.Teams.EXPECT().DeleteMember(ctx, "main", "pepito").Return(nil)
	// DeleteByTeamMember only deletes team-scoped tokens for that team
	s.ApiTokens.EXPECT().DeleteByTeamMember(ctx, "pepito", "main").Return(nil)

	err := s.S.DeleteTeamMember(ctx, "main", "pepito")
	require.NoError(t, err)

	// Personal token should still work after team removal
	s.ApiTokens.EXPECT().FindByHash(ctx, "personal-hash").Return(&apitoken.AuthResult{
		Username: "pepito",
		UserID:   2,
		Personal: true,
		TokenID:  20,
	}, nil)
	result, err := s.S.FindApiTokenByHash(ctx, "personal-hash")
	require.NoError(t, err)
	assert.True(t, result.Personal)
	assert.Equal(t, "pepito", result.Username)
}
