// Package team defines the domain model for teams in PikoCI.
// Teams organize users and pipelines, providing access control through
// membership and admin roles.
package team

import (
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/user"
)

// Team represents a group of users that owns pipelines and resources.
type Team struct {
	ID              uint32 `json:"id"`
	Name            string `json:"name"`
	Canonical       string `json:"canonical"`
	WorkerTokenSalt string `json:"-"`
}

// WithMembers embeds a Team along with its list of members.
type WithMembers struct {
	Team

	Members []Member `json:"members"`
}

// Member represents a user's membership in a team, indicating the
// user's role for that team.
type Member struct {
	Role role.Role `json:"role"`

	User user.User `json:"user"`
}
