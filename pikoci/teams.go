package pikoci

import (
	"context"
	"fmt"

	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/utils"
)

// CreateTeam creates a new team and adds the specified user as its initial admin
// member. The team's canonical name is derived from its display name.
func (q *PikoCI) CreateTeam(ctx context.Context, un string, t team.Team) (*team.WithMembers, error) {
	if !utils.ValidateCanonical(un) {
		return nil, fmt.Errorf("invalid Username format %q", un)
	} else if t.Name == "" {
		return nil, fmt.Errorf("team name is required")
	}

	t.Canonical = utils.Canonicalize(t.Name)

	var twm *team.WithMembers
	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		id, err := uow.Teams().Create(ctx, t)
		if err != nil {
			return fmt.Errorf("failed to create Team: %w", err)
		}
		t.ID = id

		err = uow.Teams().CreateMember(ctx, t.Canonical, team.Member{
			Role: role.Admin,
			User: user.User{
				Username: un,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to create Team Member: %w", err)
		}

		twm, err = uow.Teams().Find(ctx, t.Canonical)
		if err != nil {
			return fmt.Errorf("failed to find Team: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return twm, nil
}

// ListTeams returns all teams that the given user is a member of.
func (q *PikoCI) ListTeams(ctx context.Context, un string) ([]*team.WithMembers, error) {
	if !utils.ValidateCanonical(un) {
		return nil, fmt.Errorf("invalid Username format %q", un)
	}

	teams, err := q.Teams.Filter(ctx, un)
	if err != nil {
		return nil, fmt.Errorf("failed to list Teams: %w", err)
	}

	return teams, nil
}

// GetTeam retrieves a team by its canonical name, including its members.
func (q *PikoCI) GetTeam(ctx context.Context, tc string) (*team.WithMembers, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	}

	t, err := q.Teams.Find(ctx, tc)
	if err != nil {
		return nil, fmt.Errorf("failed to get Team: %w", err)
	}

	return t, nil
}

// UpdateTeam updates a team's properties and recomputes its canonical name from
// the new display name.
func (q *PikoCI) UpdateTeam(ctx context.Context, tc string, t team.Team) (*team.WithMembers, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	}

	t.Canonical = utils.Canonicalize(t.Name)

	err := q.Teams.Update(ctx, tc, t)
	if err != nil {
		return nil, fmt.Errorf("failed to update Team: %w", err)
	}

	twm, err := q.Teams.Find(ctx, t.Canonical)
	if err != nil {
		return nil, fmt.Errorf("failed to find Team: %w", err)
	}

	return twm, nil
}

// DeleteTeam removes a team by its canonical name.
func (q *PikoCI) DeleteTeam(ctx context.Context, tc string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	}

	err := q.Teams.Delete(ctx, tc)
	if err != nil {
		return fmt.Errorf("failed to delete Team: %w", err)
	}

	return nil
}

// CreateTeamMember adds a new member to the specified team and returns the
// created member.
func (q *PikoCI) CreateTeamMember(ctx context.Context, tc string, tm team.Member) (*team.Member, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(tm.User.Username) {
		return nil, fmt.Errorf("invalid Team Member Username format %q", tm.User.Username)
	}
	if !tm.Role.Assignable() {
		return nil, fmt.Errorf("invalid role %q: must be one of viewer, operator, maintainer, admin", tm.Role)
	}

	err := q.Teams.CreateMember(ctx, tc, tm)
	if err != nil {
		return nil, fmt.Errorf("failed to create member: %w", err)
	}

	rtm, err := q.Teams.FindMember(ctx, tc, tm.User.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to find member: %w", err)
	}

	return rtm, nil
}

// UpdateTeamMember updates an existing team member's role. It validates that the
// operation does not leave the team without any admin members. The validation
// and mutation are wrapped in a transaction to prevent race conditions.
func (q *PikoCI) UpdateTeamMember(ctx context.Context, tc, mu string, tm team.Member) (*team.Member, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(mu) {
		return nil, fmt.Errorf("invalid Team Member Username format %q", mu)
	}
	if !tm.Role.Assignable() {
		return nil, fmt.Errorf("invalid role %q: must be one of viewer, operator, maintainer, admin", tm.Role)
	}

	var rtm *team.Member
	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		t, err := uow.Teams().Find(ctx, tc)
		if err != nil {
			return fmt.Errorf("failed to get Team: %w", err)
		}
		if err := checkTeamAdmins(t, mu, &tm); err != nil {
			return err
		}

		err = uow.Teams().UpdateMember(ctx, tc, mu, tm)
		if err != nil {
			return fmt.Errorf("failed to update member: %w", err)
		}

		rtm, err = uow.Teams().FindMember(ctx, tc, mu)
		if err != nil {
			return fmt.Errorf("failed to find member: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return rtm, nil
}

// DeleteTeamMember removes a member from the specified team. It validates that
// the operation does not leave the team without any admin members. The validation
// and mutation are wrapped in a transaction to prevent race conditions.
func (q *PikoCI) DeleteTeamMember(ctx context.Context, tc, mu string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(mu) {
		return fmt.Errorf("invalid Team Member Username format %q", mu)
	}

	return q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		t, err := uow.Teams().Find(ctx, tc)
		if err != nil {
			return fmt.Errorf("failed to get Team: %w", err)
		}
		if err := checkTeamAdmins(t, mu, nil); err != nil {
			return err
		}

		err = uow.Teams().DeleteMember(ctx, tc, mu)
		if err != nil {
			return fmt.Errorf("failed to delete member: %w", err)
		}

		err = uow.ApiTokens().DeleteByTeamMember(ctx, mu, tc)
		if err != nil {
			return fmt.Errorf("failed to delete API tokens for removed member: %w", err)
		}

		return nil
	})
}

// checkTeamAdmins checks that the team would still have at least one admin
// after the proposed change. If m is non-nil, it simulates updating the member;
// otherwise it simulates removing the member.
func checkTeamAdmins(t *team.WithMembers, mu string, m *team.Member) error {
	var admins int
	for _, tm := range t.Members {
		if tm.User.Username == mu {
			if m != nil {
				tm = *m
			} else {
				continue
			}
		}
		if tm.Role == role.Admin {
			admins++
		}
	}

	if admins == 0 {
		return fmt.Errorf("cannot have a team with no admins")
	}
	return nil
}
