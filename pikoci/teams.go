package pikoci

import (
	"context"
	"fmt"

	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/utils"
)

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
			Admin: true,
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

func (q *PikoCI) CreateTeamMember(ctx context.Context, tc string, tm team.Member) (*team.Member, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(tm.User.Username) {
		return nil, fmt.Errorf("invalid Team Member Username format %q", tm.User.Username)
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

func (q *PikoCI) UpdateTeamMember(ctx context.Context, tc, mu string, tm team.Member) (*team.Member, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(mu) {
		return nil, fmt.Errorf("invalid Team Member Username format %q", mu)
	} else if err := q.validateTeamAdmins(ctx, tc, mu, &tm); err != nil {
		return nil, err
	}

	err := q.Teams.UpdateMember(ctx, tc, mu, tm)
	if err != nil {
		return nil, fmt.Errorf("failed to update member: %w", err)
	}

	rtm, err := q.Teams.FindMember(ctx, tc, mu)
	if err != nil {
		return nil, fmt.Errorf("failed to find member: %w", err)
	}

	return rtm, nil
}

func (q *PikoCI) DeleteTeamMember(ctx context.Context, tc, mu string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(mu) {
		return fmt.Errorf("invalid Team Member Username format %q", mu)
	} else if err := q.validateTeamAdmins(ctx, tc, mu, nil); err != nil {
		return err
	}

	err := q.Teams.DeleteMember(ctx, tc, mu)
	if err != nil {
		return fmt.Errorf("failed to delete member: %w", err)
	}

	return nil
}

func (q *PikoCI) validateTeamAdmins(ctx context.Context, tc, mu string, m *team.Member) error {
	t, err := q.Teams.Find(ctx, tc)
	if err != nil {
		return fmt.Errorf("failed to get Team: %w", err)
	}

	var admins int
	for _, tm := range t.Members {
		if tm.User.Username == mu && m != nil {
			tm = *m
		}
		if tm.Admin {
			admins++
		}
	}

	if admins == 0 {
		return fmt.Errorf("cannot have a team with no admins")
	}
	return nil
}
