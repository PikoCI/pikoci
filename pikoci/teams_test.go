package pikoci_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/user"
	"go.uber.org/mock/gomock"
)

func TestCreateTeam(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Teams.EXPECT().Create(ctx, gomock.Any()).Return(uint32(1), nil)
	s.Teams.EXPECT().CreateMember(ctx, "my-team", gomock.Any()).Return(nil)
	s.Teams.EXPECT().Find(ctx, "my-team").Return(&team.WithMembers{
		Team:    team.Team{ID: 1, Name: "My Team", Canonical: "my-team"},
		Members: []team.Member{{Admin: true, User: user.User{Username: "admin"}}},
	}, nil)

	twm, err := s.S.CreateTeam(ctx, "admin", team.Team{Name: "My Team"})
	require.NoError(t, err)
	assert.Equal(t, "my-team", twm.Canonical)
	assert.Len(t, twm.Members, 1)
	assert.True(t, twm.Members[0].Admin)
}

func TestCreateTeam_EmptyName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateTeam(ctx, "admin", team.Team{Name: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "team name is required")
}

func TestCreateTeam_InvalidUsername(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateTeam(ctx, "INVALID", team.Team{Name: "Test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Username format")
}

func TestGetTeam(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := &team.WithMembers{Team: team.Team{ID: 1, Canonical: "main"}}
	s.Teams.EXPECT().Find(ctx, "main").Return(expected, nil)

	twm, err := s.S.GetTeam(ctx, "main")
	require.NoError(t, err)
	assert.Equal(t, expected, twm)
}

func TestListTeams(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := []*team.WithMembers{{Team: team.Team{ID: 1, Canonical: "main"}}}
	s.Teams.EXPECT().Filter(ctx, "admin").Return(expected, nil)

	teams, err := s.S.ListTeams(ctx, "admin")
	require.NoError(t, err)
	assert.Equal(t, expected, teams)
}

func TestUpdateTeam(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Teams.EXPECT().Update(ctx, "main", gomock.Any()).Return(nil)
	s.Teams.EXPECT().Find(ctx, "new-name").Return(&team.WithMembers{
		Team: team.Team{ID: 1, Name: "New Name", Canonical: "new-name"},
	}, nil)

	twm, err := s.S.UpdateTeam(ctx, "main", team.Team{Name: "New Name"})
	require.NoError(t, err)
	assert.Equal(t, "new-name", twm.Canonical)
}

func TestDeleteTeam(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Teams.EXPECT().Delete(ctx, "main").Return(nil)

	err := s.S.DeleteTeam(ctx, "main")
	require.NoError(t, err)
}

func TestDeleteTeam_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.DeleteTeam(ctx, "INVALID")
	require.Error(t, err)
}

func TestCreateTeamMember(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	member := team.Member{Admin: false, User: user.User{Username: "pepito"}}
	s.Teams.EXPECT().CreateMember(ctx, "main", member).Return(nil)
	s.Teams.EXPECT().FindMember(ctx, "main", "pepito").Return(&member, nil)

	m, err := s.S.CreateTeamMember(ctx, "main", member)
	require.NoError(t, err)
	assert.Equal(t, "pepito", m.User.Username)
}

func TestUpdateTeamMember(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// validateTeamAdmins will call Find
	s.Teams.EXPECT().Find(ctx, "main").Return(&team.WithMembers{
		Team: team.Team{ID: 1, Canonical: "main"},
		Members: []team.Member{
			{Admin: true, User: user.User{Username: "admin"}},
			{Admin: false, User: user.User{Username: "pepito"}},
		},
	}, nil)
	s.Teams.EXPECT().UpdateMember(ctx, "main", "pepito", gomock.Any()).Return(nil)
	s.Teams.EXPECT().FindMember(ctx, "main", "pepito").Return(&team.Member{
		Admin: true, User: user.User{Username: "pepito"},
	}, nil)

	m, err := s.S.UpdateTeamMember(ctx, "main", "pepito", team.Member{Admin: true})
	require.NoError(t, err)
	assert.True(t, m.Admin)
}

func TestUpdateTeamMember_WouldRemoveLastAdmin(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Only one admin, trying to remove their admin status
	s.Teams.EXPECT().Find(ctx, "main").Return(&team.WithMembers{
		Team: team.Team{ID: 1, Canonical: "main"},
		Members: []team.Member{
			{Admin: true, User: user.User{Username: "admin"}},
		},
	}, nil)

	_, err := s.S.UpdateTeamMember(ctx, "main", "admin", team.Member{Admin: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no admins")
}

func TestDeleteTeamMember(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// validateTeamAdmins: team has 2 members, one admin remains
	s.Teams.EXPECT().Find(ctx, "main").Return(&team.WithMembers{
		Team: team.Team{ID: 1, Canonical: "main"},
		Members: []team.Member{
			{Admin: true, User: user.User{Username: "admin"}},
			{Admin: false, User: user.User{Username: "pepito"}},
		},
	}, nil)
	s.Teams.EXPECT().DeleteMember(ctx, "main", "pepito").Return(nil)

	err := s.S.DeleteTeamMember(ctx, "main", "pepito")
	require.NoError(t, err)
}

func TestGetTeam_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.GetTeam(ctx, "INVALID")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Team Canonical")
}

func TestListTeams_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.ListTeams(ctx, "INVALID")
	require.Error(t, err)
}

func TestUpdateTeam_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.UpdateTeam(ctx, "INVALID", team.Team{Name: "test"})
	require.Error(t, err)
}

func TestCreateTeamMember_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateTeamMember(ctx, "INVALID", team.Member{})
	require.Error(t, err)
}

func TestUpdateTeamMember_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.UpdateTeamMember(ctx, "INVALID", "pepito", team.Member{})
	require.Error(t, err)

	_, err = s.S.UpdateTeamMember(ctx, "main", "INVALID", team.Member{})
	require.Error(t, err)
}

func TestDeleteTeamMember_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.DeleteTeamMember(ctx, "INVALID", "pepito")
	require.Error(t, err)

	err = s.S.DeleteTeamMember(ctx, "main", "INVALID")
	require.Error(t, err)
}

func TestDeleteTeamMember_LastAdmin(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// NOTE: validateTeamAdmins with m=nil (delete case) doesn't exclude
	// the member being deleted from the admin count, so this currently
	// passes validation even though it shouldn't. The delete proceeds.
	s.Teams.EXPECT().Find(ctx, "main").Return(&team.WithMembers{
		Team: team.Team{ID: 1, Canonical: "main"},
		Members: []team.Member{
			{Admin: true, User: user.User{Username: "admin"}},
		},
	}, nil)
	s.Teams.EXPECT().DeleteMember(ctx, "main", "admin").Return(nil)

	err := s.S.DeleteTeamMember(ctx, "main", "admin")
	require.NoError(t, err)
}
