package pikoci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
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
	if b.Status != build.WaitingForApproval {
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
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("you have already voted on this build")
		}
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
	if b.Status != build.WaitingForApproval {
		return fmt.Errorf("build %s is not waiting for approval (status: %s)", buildNumber, b.Status)
	}

	if err := q.Builds.CreateApproval(ctx, b.ID, username, "rejected", message); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("you have already voted on this build")
		}
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

// FireApproveNotifications sends notifications configured in the job's approve
// block when a build enters WaitingForApproval. The message is interpolated with
// build metadata. This is fire-and-forget: errors are logged, not returned.
func (q *PikoCI) FireApproveNotifications(ctx context.Context, tc, pc, jn string, j *job.Job, buildNumber string) {
	// ApproveNotify may be empty if the job was loaded from DB (which doesn't
	// persist it). Re-read from the pipeline's raw HCL to get the full config.
	if len(j.ApproveNotify) == 0 {
		pp, pErr := q.Pipelines.Find(ctx, tc, pc)
		if pErr == nil && pp.Raw != nil {
			parsed, rErr := ReadPipeline(ctx, pp.Raw, nil)
			if rErr == nil {
				for _, pj := range parsed.Jobs {
					if pj.Name == jn {
						j = &pj
						break
					}
				}
			}
		}
	}
	if len(j.ApproveNotify) == 0 {
		return
	}
	pp, err := q.Pipelines.Find(ctx, tc, pc)
	if err != nil {
		q.logger.Error("approve notify: failed to find pipeline", "error", err)
		return
	}
	for _, ns := range j.ApproveNotify {
		nCan := utils.NotificationCanonical(ns.Type, ns.Name)
		notif, ok := pp.Notification(nCan)
		if !ok {
			q.logger.Error("approve notify: notification not found", "canonical", nCan)
			continue
		}
		params := notif.GetParams()
		webhookURL := params["webhook_url"]
		if webhookURL == "" {
			q.logger.Error("approve notify: missing webhook_url", "canonical", nCan)
			continue
		}

		// Interpolate message with build variables
		msg := ns.Message
		msg = strings.ReplaceAll(msg, "$BUILD_NUMBER", buildNumber)
		msg = strings.ReplaceAll(msg, "$BUILD_PIPELINE_NAME", pc)
		msg = strings.ReplaceAll(msg, "$BUILD_JOB_NAME", jn)
		msg = strings.ReplaceAll(msg, "$BUILD_TEAM_NAME", tc)
		if msg == "" {
			msg = fmt.Sprintf("⏳ [%s/%s] Build #%s needs approval", pc, jn, buildNumber)
		}

		body, _ := json.Marshal(map[string]string{"content": msg})
		resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
		if err != nil {
			q.logger.Error("approve notify: failed to send", "canonical", nCan, "error", err)
			continue
		}
		resp.Body.Close()
		q.logger.Info("approve notify: sent", "canonical", nCan, "build_number", buildNumber)
	}
}
