package pikoci

import (
	"context"
	"fmt"

	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/utils"
)

// ApproveBuild records an approval vote for a build that is waiting for approval.
// Once the required number of approvals is reached, the build transitions to Pending.
func (q *PikoCI) ApproveBuild(ctx context.Context, tc, pc, jn, buildNumber, username, message string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	b, err := q.Builds.Find(ctx, tc, pc, jn, buildNumber)
	if err != nil {
		return fmt.Errorf("failed to Find Build: %w", err)
	}
	if b.Status != build.WaitingApproval {
		return fmt.Errorf("build %s is not waiting for approval (status: %s)", buildNumber, b.Status)
	}

	j, err := q.Jobs.Find(ctx, tc, pc, jn)
	if err != nil {
		return fmt.Errorf("failed to Find Job: %w", err)
	}
	if j.ApproveLabel == "" {
		return fmt.Errorf("job %q does not have an approval gate", jn)
	}

	if err := q.Builds.CreateApproval(ctx, b.ID, username, "approved", message); err != nil {
		return fmt.Errorf("failed to record approval: %w", err)
	}

	q.audit(ctx, tc, auditlog.BuildApproved, "job", pc+"/"+jn,
		map[string]interface{}{"build_number": buildNumber, "message": message})

	// Check if we have enough approvals to proceed
	count, err := q.Builds.CountApprovals(ctx, b.ID)
	if err != nil {
		return fmt.Errorf("failed to count approvals: %w", err)
	}

	if count >= j.ApproveCount {
		b.Status = build.Pending
		if err := q.Builds.Update(ctx, tc, pc, jn, buildNumber, *b); err != nil {
			return fmt.Errorf("failed to transition build to pending: %w", err)
		}
		q.Notifier.Notify()
	}

	return nil
}

// RejectBuild records a rejection vote and immediately fails the build.
func (q *PikoCI) RejectBuild(ctx context.Context, tc, pc, jn, buildNumber, username, message string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}
	if message == "" {
		return fmt.Errorf("rejection message is required")
	}

	b, err := q.Builds.Find(ctx, tc, pc, jn, buildNumber)
	if err != nil {
		return fmt.Errorf("failed to Find Build: %w", err)
	}
	if b.Status != build.WaitingApproval {
		return fmt.Errorf("build %s is not waiting for approval (status: %s)", buildNumber, b.Status)
	}

	if err := q.Builds.CreateApproval(ctx, b.ID, username, "rejected", message); err != nil {
		return fmt.Errorf("failed to record rejection: %w", err)
	}

	b.Status = build.Failed
	b.Error = fmt.Sprintf("rejected by %s: %s", username, message)
	if err := q.Builds.Update(ctx, tc, pc, jn, buildNumber, *b); err != nil {
		return fmt.Errorf("failed to fail build: %w", err)
	}

	q.audit(ctx, tc, auditlog.BuildRejected, "job", pc+"/"+jn,
		map[string]interface{}{"build_number": buildNumber, "message": message})

	return nil
}
