package user

type User struct {
	ID       uint32 `json:"id"`
	FullName string `json:"full_name"`
	Username string `json:"username"`
	Password string `json:"-"`
	Admin    bool   `json:"admin"`
}

type WithMemberships struct {
	User

	Memberships        []Member `json:"memberships"`
	MustChangePassword bool     `json:"must_change_password,omitempty"`
}

type Member struct {
	Admin         bool   `json:"admin"`
	TeamCanonical string `json:"team_canonical"`
}

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
