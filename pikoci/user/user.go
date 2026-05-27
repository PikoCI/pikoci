// Package user defines the domain model for users in PikoCI.
// Users authenticate to the system, belong to teams, and may have
// global or team-level admin privileges.
package user

// User represents an authenticated user of the system. The Password field
// is excluded from JSON serialization for security.
type User struct {
	ID       uint32 `json:"id"`
	FullName string `json:"full_name"`
	Username string `json:"username"`
	Password string `json:"-"`
	Admin    bool   `json:"admin"`
}

// WithMemberships embeds a User along with their team memberships and a flag
// indicating whether the user must change their password on next login.
type WithMemberships struct {
	User

	Memberships        []Member `json:"memberships"`
	MustChangePassword bool     `json:"must_change_password,omitempty"`
}

// Member represents a user's membership in a specific team, identified by the
// team's canonical name.
type Member struct {
	Admin         bool   `json:"admin"`
	TeamCanonical string `json:"team_canonical"`
}

// IsAdmin reports whether the user is a global admin or an admin of any of
// the teams identified by the given canonical names.
func (u *WithMemberships) IsAdmin(tcs ...string) bool {
	if u.Admin {
		return true
	}
	for _, tc := range tcs {
		if tc == "" {
			continue
		}
		for _, m := range u.Memberships {
			if m.Admin && m.TeamCanonical == tc {
				return true
			}
		}
	}
	return false
}

// IsMember reports whether the user is a global admin or a member of any of
// the teams identified by the given canonical names. An empty canonical name
// is treated as a wildcard match.
func (u *WithMemberships) IsMember(tcs ...string) bool {
	if u.Admin {
		return true
	}
	if len(tcs) == 1 && tcs[0] == "" {
		// In case it's only an empty one it's member
		return true
	}
	for _, tc := range tcs {
		if tc == "" {
			continue
		}
		for _, m := range u.Memberships {
			if m.TeamCanonical == tc {
				return true
			}
		}
	}
	return false
}
