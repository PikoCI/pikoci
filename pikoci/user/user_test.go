package user_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/pikoci/pikoci/pikoci/user"
)

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
				{Admin: true, TeamCanonical: "team-a"},
				{Admin: false, TeamCanonical: "team-b"},
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
				{Admin: true, TeamCanonical: "team-a"},
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
				{Admin: false, TeamCanonical: "team-a"},
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
