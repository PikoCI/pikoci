package user_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/user"
)

func TestWithMemberships_HasRole(t *testing.T) {
	t.Run("global admin always passes", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: true},
		}
		assert.True(t, u.HasRole(role.Admin))
		assert.True(t, u.HasRole(role.Admin, "any-team"))
		assert.True(t, u.HasRole(role.Read, "any-team"))
	})

	t.Run("admin has all roles", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: false},
			Memberships: []user.Member{
				{Role: role.Admin, TeamCanonical: "team-a"},
			},
		}
		assert.True(t, u.HasRole(role.Admin, "team-a"))
		assert.True(t, u.HasRole(role.Maintain, "team-a"))
		assert.True(t, u.HasRole(role.Write, "team-a"))
		assert.True(t, u.HasRole(role.Read, "team-a"))
		assert.False(t, u.HasRole(role.Admin, "team-b"))
	})

	t.Run("viewer cannot do operator actions", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: false},
			Memberships: []user.Member{
				{Role: role.Read, TeamCanonical: "team-a"},
			},
		}
		assert.True(t, u.HasRole(role.Read, "team-a"))
		assert.False(t, u.HasRole(role.Write, "team-a"))
	})

	t.Run("empty team canonical is skipped", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: false},
			Memberships: []user.Member{
				{Role: role.Admin, TeamCanonical: "team-a"},
			},
		}
		assert.False(t, u.HasRole(role.Read, ""))
	})

	t.Run("no memberships", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: false},
		}
		assert.False(t, u.HasRole(role.Read, "team-a"))
	})

	t.Run("multi-team membership", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: false},
			Memberships: []user.Member{
				{Role: role.Write, TeamCanonical: "team-a"},
				{Role: role.Admin, TeamCanonical: "team-b"},
			},
		}
		assert.True(t, u.HasRole(role.Write, "team-a"))
		assert.False(t, u.HasRole(role.Maintain, "team-a"))
		assert.True(t, u.HasRole(role.Admin, "team-b"))
	})
}

func TestWithMemberships_IsAdmin(t *testing.T) {
	t.Run("global admin is always admin", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: true},
		}
		assert.True(t, u.IsAdmin())
		assert.True(t, u.IsAdmin("any-team"))
	})

	t.Run("team admin for specific team", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: false},
			Memberships: []user.Member{
				{Role: role.Admin, TeamCanonical: "team-a"},
				{Role: role.Maintain, TeamCanonical: "team-b"},
			},
		}
		assert.True(t, u.IsAdmin("team-a"))
		assert.False(t, u.IsAdmin("team-b"))
		assert.False(t, u.IsAdmin("team-c"))
	})

	t.Run("non-admin with no memberships", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: false},
		}
		assert.False(t, u.IsAdmin())
		assert.False(t, u.IsAdmin("team-a"))
	})

	t.Run("empty team canonical is skipped", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: false},
			Memberships: []user.Member{
				{Role: role.Admin, TeamCanonical: "team-a"},
			},
		}
		assert.False(t, u.IsAdmin(""))
	})
}

func TestWithMemberships_IsMember(t *testing.T) {
	t.Run("global admin is always member", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: true},
		}
		assert.True(t, u.IsMember("any-team"))
	})

	t.Run("member of specific team", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: false},
			Memberships: []user.Member{
				{Role: role.Read, TeamCanonical: "team-a"},
			},
		}
		assert.True(t, u.IsMember("team-a"))
		assert.False(t, u.IsMember("team-b"))
	})

	t.Run("empty string only means member", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: false},
		}
		assert.True(t, u.IsMember(""))
	})

	t.Run("non-member of any team", func(t *testing.T) {
		u := &user.WithMemberships{
			User: user.User{Admin: false},
		}
		assert.False(t, u.IsMember("team-a"))
	})
}
