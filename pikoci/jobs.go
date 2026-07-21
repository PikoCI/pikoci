package pikoci

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/utils"
)

// TriggerPipelineJob creates a pending build for the specified job, pins the
// latest version of its first get-step resource, and enqueues the build for
// execution via the job topic.
func (q *PikoCI) TriggerPipelineJob(ctx context.Context, tc, pc, jn string, inputValues map[string]string, manual bool) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	j, err := q.Jobs.Find(ctx, tc, pc, jn)
	if err != nil {
		return fmt.Errorf("failed to Find Job %q on Pipeline %q: %w", jn, pc, err)
	}

	if j.Paused {
		return fmt.Errorf("job %q is paused", jn)
	}

	resolved, err := resolveInputValues(j.Inputs, inputValues, manual)
	if err != nil {
		return fmt.Errorf("invalid input values for job %q: %w", jn, err)
	}

	bb := build.Build{Status: build.Pending, InputValues: resolved}

	// Use the pinned version if the resource is pinned, otherwise use the
	// latest version of the first get-step resource.
	getSteps := j.GetSteps()
	if len(getSteps) > 0 {
		g := getSteps[0]
		rCan := g.ResourceCanonical()
		r, rerr := q.Resources.Find(ctx, tc, pc, rCan)
		if rerr == nil && r.PinnedVersionID != nil {
			bb.ResourceCanonical = rCan
			bb.VersionID = *r.PinnedVersionID
		} else {
			vers, err := q.Resources.FilterVersions(ctx, tc, pc, rCan, nil, nil, 0)
			if err == nil && len(vers) > 0 {
				bb.ResourceCanonical = rCan
				bb.VersionID = vers[0].ID
			}
		}
	}

	_, err = q.CreateJobBuild(ctx, tc, pc, jn, bb)
	if err != nil {
		return fmt.Errorf("failed to create pending build for Job %q on Pipeline %q: %w", jn, pc, err)
	}

	q.Notifier.Notify()

	q.audit(ctx, tc, auditlog.JobTriggered, "job", pc+"/"+jn, map[string]interface{}{"pipeline": pc})
	return nil
}

// PauseJob pauses a specific job within a pipeline.
func (q *PikoCI) PauseJob(ctx context.Context, tc, pc, jn string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}
	err := q.Jobs.SetPaused(ctx, tc, pc, jn, true)
	if err != nil {
		return err
	}
	q.audit(ctx, tc, auditlog.JobPaused, "job", pc+"/"+jn, map[string]interface{}{"pipeline": pc})
	return nil
}

// UnpauseJob unpauses a specific job within a pipeline.
func (q *PikoCI) UnpauseJob(ctx context.Context, tc, pc, jn string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}
	err := q.Jobs.SetPaused(ctx, tc, pc, jn, false)
	if err != nil {
		return err
	}
	q.audit(ctx, tc, auditlog.JobUnpaused, "job", pc+"/"+jn, map[string]interface{}{"pipeline": pc})
	return nil
}

// ListPipelineJobs returns all jobs for the given pipeline enriched with
// their latest build status information.
func (q *PikoCI) ListPipelineJobs(ctx context.Context, tc, pn string) ([]job.WithStatus, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pn)
	}

	pp, err := q.Pipelines.Find(ctx, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to get Pipeline %q: %w", pn, err)
	}

	return q.enrichJobsWithStatus(ctx, tc, pn, pp.Jobs)
}

// ListPublicPipelineJobs returns all jobs for a public pipeline enriched
// with their latest build status.
func (q *PikoCI) ListPublicPipelineJobs(ctx context.Context, tc, pn string) ([]job.WithStatus, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pn)
	}

	pp, err := q.Pipelines.FindPublic(ctx, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	return q.enrichJobsWithStatus(ctx, tc, pn, pp.Jobs)
}

// enrichJobsWithStatus enriches a slice of jobs with build status information.
// It pre-fetches active and completed builds for all jobs in two batch queries
// instead of making per-job calls.
func (q *PikoCI) enrichJobsWithStatus(ctx context.Context, tc, pn string, jobs []job.Job) ([]job.WithStatus, error) {
	activeByJob, err := q.Builds.FilterByPipeline(ctx, tc, pn, []build.Status{build.Started, build.Pending})
	if err != nil {
		return nil, fmt.Errorf("failed to filter active builds: %w", err)
	}
	completedByJob, err := q.Builds.FilterByPipeline(ctx, tc, pn, []build.Status{build.Succeeded, build.Failed, build.Cancelled, build.WaitingForApproval, build.Warning})
	if err != nil {
		return nil, fmt.Errorf("failed to filter completed builds: %w", err)
	}

	result := make([]job.WithStatus, 0, len(jobs))
	for _, j := range jobs {
		ws := job.WithStatus{Job: j}

		if completed := completedByJob[j.Name]; len(completed) > 0 {
			cb := completed[0]
			ws.LatestStatus = cb.Status.String()
			ws.LatestBuildNumber = cb.BuildNumber
			ws.LatestBuildDuration = int64(cb.Duration)
			if !cb.StartedAt.IsZero() {
				t := cb.StartedAt
				ws.StartedAt = &t
			}
		}

		if active := activeByJob[j.Name]; len(active) > 0 {
			ws.HasRunning = true
			if len(completedByJob[j.Name]) == 0 {
				ws.LatestBuildNumber = active[0].BuildNumber
				if !active[0].StartedAt.IsZero() {
					t := active[0].StartedAt
					ws.StartedAt = &t
				}
			}
		}

		result = append(result, ws)
	}
	return result, nil
}

// GetPipelineJob retrieves a job by its name within a pipeline.
func (q *PikoCI) GetPipelineJob(ctx context.Context, tc, pc, jn string) (*job.Job, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	j, err := q.Jobs.Find(ctx, tc, pc, jn)
	if err != nil {
		return nil, fmt.Errorf("failed to Find Job %q on Pipeline %q: %w", jn, pc, err)
	}

	return j, nil
}

// resolveInputValues validates and resolves input values against the declared
// inputs. For manual triggers, required inputs (no default) must be provided.
// For automatic triggers, missing values use defaults or zero values.
func resolveInputValues(inputs []job.Input, provided map[string]string, manual bool) (map[string]string, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	declared := make(map[string]job.Input, len(inputs))
	for _, inp := range inputs {
		declared[inp.Name] = inp
	}

	// Reject unknown keys
	for k := range provided {
		if _, ok := declared[k]; !ok {
			return nil, fmt.Errorf("unknown input %q", k)
		}
	}

	resolved := make(map[string]string, len(inputs))
	for _, inp := range inputs {
		val, ok := provided[inp.Name]
		if !ok || val == "" {
			if inp.Default != nil {
				val = *inp.Default
			} else if manual {
				return nil, fmt.Errorf("required input %q not provided", inp.Name)
			} else {
				// Auto-trigger zero values
				switch inp.Type {
				case "number":
					val = "0"
				case "bool":
					val = "false"
				default:
					val = ""
				}
			}
		}

		// Type validation
		switch inp.Type {
		case "number":
			if _, err := strconv.ParseFloat(val, 64); err != nil {
				return nil, fmt.Errorf("input %q: value %q is not a valid number", inp.Name, val)
			}
		case "bool":
			if val != "true" && val != "false" {
				return nil, fmt.Errorf("input %q: value %q must be \"true\" or \"false\"", inp.Name, val)
			}
		}

		// Options validation
		if len(inp.Options) > 0 && val != "" {
			if inp.Multiple {
				parts := strings.Split(val, ",")
				for _, p := range parts {
					p = strings.TrimSpace(p)
					found := false
					for _, o := range inp.Options {
						if o == p {
							found = true
							break
						}
					}
					if !found {
						return nil, fmt.Errorf("input %q: value %q is not in options %v", inp.Name, p, inp.Options)
					}
				}
			} else {
				found := false
				for _, o := range inp.Options {
					if o == val {
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("input %q: value %q is not in options %v", inp.Name, val, inp.Options)
				}
			}
		}

		resolved[inp.Name] = val
	}

	return resolved, nil
}
