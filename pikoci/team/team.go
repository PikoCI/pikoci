// Package team defines the domain model for teams in PikoCI.
// Teams organize users and pipelines, providing access control through
// membership and admin roles.
package team

import (
	"github.com/xescugc/pikoci/pikoci/user"
)

// Team represents a group of users that owns pipelines and resources.
type Team struct {
	ID        uint32 `json:"id"`
	Name      string `json:"name"`
	Canonical string `json:"canonical"`
}

// WithMembers embeds a Team along with its list of members.
type WithMembers struct {
	Team

	Members []Member `json:"members"`
}

// Member represents a user's membership in a team, indicating whether the
// user has admin privileges for that team.
type Member struct {
	Admin bool `json:"admin"`

	User user.User `json:"user"`
}
