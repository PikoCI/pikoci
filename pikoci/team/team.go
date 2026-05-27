package team

import (
	"github.com/pikoci/pikoci/pikoci/user"
)

type Team struct {
	ID        uint32 `json:"id"`
	Name      string `json:"name"`
	Canonical string `json:"canonical"`
}

type WithMembers struct {
	Team

	Members []Member `json:"members"`
}

type Member struct {
	Admin bool `json:"admin"`

	User user.User `json:"user"`
}
