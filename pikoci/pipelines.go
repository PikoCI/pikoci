package pikoci

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/awalterschulze/gographviz"

	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/notification"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/scheduler"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/utils"
)

// CreatePipeline parses the raw HCL configuration and creates a new pipeline
// along with all its jobs, resources, resource types, runners, and secret types
// within a single unit of work.
func (q *PikoCI) CreatePipeline(ctx context.Context, tc, pn string, rpp []byte, vars map[string]interface{}) (*pipeline.Pipeline, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	}

	pCan := utils.Canonicalize(pn)
	if !utils.ValidateCanonical(pCan) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pn)
	}

	pp, err := ReadPipeline(ctx, rpp, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to read Pipeline config: %w", err)
	}

	pp.Name = pn
	pp.Canonical = pCan
	pp.Raw = rpp

	var cp *pipeline.Pipeline
	err = q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		_, err := uow.Pipelines().Create(ctx, tc, *pp)
		if err != nil {
			return fmt.Errorf("failed to create Pipeline %q: %w", pn, err)
		}

		for _, j := range pp.Jobs {
			if !utils.ValidateCanonical(j.Name) {
				return fmt.Errorf("invalid Job Name format %q", j.Name)
			}
			_, err = uow.Jobs().Create(ctx, tc, pCan, j)
			if err != nil {
				return fmt.Errorf("failed to create Job %q: %w", j.Name, err)
			}
		}

		for _, rt := range pp.ResourceTypes {
			if !utils.ValidateCanonical(rt.Name) {
				return fmt.Errorf("invalid ResourceType Name format %q", rt.Name)
			}
			_, err = uow.ResourceTypes().Create(ctx, tc, pCan, rt)
			if err != nil {
				return fmt.Errorf("failed to create ResourceType %q: %w", rt.Name, err)
			}
		}

		for _, r := range pp.Resources {
			if !utils.ValidateCanonical(r.Name) {
				return fmt.Errorf("invalid Resource Name format %q", r.Name)
			}
			spec := r.CheckInterval
			if spec == "" {
				spec = "@every 1m"
			}
			if err := scheduler.ValidateCheckInterval(spec); err != nil {
				return fmt.Errorf("invalid check_interval for resource %q: %w", r.Name, err)
			}
			nextCheck, err := scheduler.ComputeNextCheck(spec, time.Now())
			if err != nil {
				return fmt.Errorf("failed to compute next check for resource %q: %w", r.Name, err)
			}
			r.NextCheck = nextCheck
			r.WebhookToken, err = newWebhookToken(r.Canonical)
			if err != nil {
				return fmt.Errorf("failed to generate webhook token for resource %q: %w", r.Name, err)
			}
			_, err = uow.Resources().Create(ctx, tc, pCan, r)
			if err != nil {
				return fmt.Errorf("failed to create Resource %q: %w", r.Name, err)
			}
		}

		for _, ru := range pp.Runners {
			if !utils.ValidateCanonical(ru.Name) {
				return fmt.Errorf("invalid Runner Name format %q", ru.Name)
			}
			_, err = uow.Runners().Create(ctx, tc, pCan, ru)
			if err != nil {
				return fmt.Errorf("failed to create Runner %q: %w", ru.Name, err)
			}
		}

		for _, st := range pp.SecretTypes {
			if !utils.ValidateCanonical(st.Name) {
				return fmt.Errorf("invalid SecretType Name format %q", st.Name)
			}
			_, err = uow.SecretTypes().Create(ctx, tc, pCan, st)
			if err != nil {
				return fmt.Errorf("failed to create SecretType %q: %w", st.Name, err)
			}
		}

		for _, nt := range pp.NotificationTypes {
			if !utils.ValidateCanonical(nt.Name) {
				return fmt.Errorf("invalid NotificationType Name format %q", nt.Name)
			}
			_, err = uow.NotificationTypes().Create(ctx, tc, pCan, nt)
			if err != nil {
				return fmt.Errorf("failed to create NotificationType %q: %w", nt.Name, err)
			}
		}

		for _, n := range pp.Notifications {
			if !utils.ValidateCanonical(n.Name) {
				return fmt.Errorf("invalid Notification Name format %q", n.Name)
			}
			_, err = uow.Notifications().Create(ctx, tc, pCan, n)
			if err != nil {
				return fmt.Errorf("failed to create Notification %q: %w", n.Canonical, err)
			}
		}

		cp, err = uow.Pipelines().Find(ctx, tc, pCan)
		if err != nil {
			return fmt.Errorf("failed to get Pipeline: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	q.audit(ctx, tc, auditlog.PipelineCreated, "pipeline", cp.Canonical, nil)
	return cp, nil
}

// UpdatePipeline replaces an existing pipeline's configuration. It performs a
// diff-based reconciliation of jobs, resources, resource types, runners, and
// secret types, creating, updating, or deleting entities as needed. An optional
// newName parameter renames the pipeline.
func (q *PikoCI) UpdatePipeline(ctx context.Context, tc, pCan string, rpp []byte, vars map[string]interface{}, newName ...string) (*pipeline.Pipeline, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}

	pp, err := ReadPipeline(ctx, rpp, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to read Pipeline config: %w", err)
	}

	// Fetch existing pipeline to preserve the display name
	existingPP, err := q.Pipelines.Find(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("failed to find Pipeline %q: %w", pCan, err)
	}

	if len(newName) > 0 && newName[0] != "" {
		pp.Name = newName[0]
		pp.Canonical = utils.Canonicalize(newName[0])
		if !utils.ValidateCanonical(pp.Canonical) {
			return nil, fmt.Errorf("invalid Pipeline Name format %q", newName[0])
		}
	} else {
		pp.Name = existingPP.Name
		pp.Canonical = pCan
	}
	pp.Raw = rpp

	newCan := pp.Canonical
	var up *pipeline.Pipeline
	err = q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		err := uow.Pipelines().Update(ctx, tc, pCan, *pp)
		if err != nil {
			return fmt.Errorf("failed to update Pipeline %q: %w", pCan, err)
		}

		// After update, use the new canonical for all subsequent operations
		pCan = newCan

		dbpp, err := uow.Pipelines().Find(ctx, tc, pCan)
		if err != nil {
			return fmt.Errorf("failed to get Pipeline %q: %w", pCan, err)
		}

		dbjbs := make(map[string]struct{})
		for _, j := range dbpp.Jobs {
			dbjbs[j.Name] = struct{}{}
		}
		for _, j := range pp.Jobs {
			if !utils.ValidateCanonical(j.Name) {
				return fmt.Errorf("invalid Job Name format %q", j.Name)
			}
			if _, ok := dbjbs[j.Name]; ok {
				delete(dbjbs, j.Name)
				err = uow.Jobs().Update(ctx, tc, pCan, j.Name, j)
				if err != nil {
					return fmt.Errorf("failed to update Job %q: %w", j.Name, err)
				}
			} else {
				_, err = uow.Jobs().Create(ctx, tc, pCan, j)
				if err != nil {
					return fmt.Errorf("failed to create Job %q: %w", j.Name, err)
				}
			}
		}
		for jn := range dbjbs {
			err = uow.Jobs().Delete(ctx, tc, pCan, jn)
			if err != nil {
				return fmt.Errorf("failed to delete Job %q: %w", jn, err)
			}
		}

		dbrts := make(map[string]struct{})
		for _, rt := range dbpp.ResourceTypes {
			dbrts[rt.Name] = struct{}{}
		}
		for _, rt := range pp.ResourceTypes {
			if !utils.ValidateCanonical(rt.Name) {
				return fmt.Errorf("invalid ResourceType Name format %q", rt.Name)
			}
			if _, ok := dbrts[rt.Name]; ok {
				delete(dbrts, rt.Name)
				err = uow.ResourceTypes().Update(ctx, tc, pCan, rt.Name, rt)
				if err != nil {
					return fmt.Errorf("failed to update ResourceType %q: %w", rt.Name, err)
				}
			} else {
				_, err = uow.ResourceTypes().Create(ctx, tc, pCan, rt)
				if err != nil {
					return fmt.Errorf("failed to create ResourceType %q: %w", rt.Name, err)
				}
			}
		}
		for rt := range dbrts {
			err = uow.ResourceTypes().Delete(ctx, tc, pCan, rt)
			if err != nil {
				return fmt.Errorf("failed to delete ResourceType %q: %w", rt, err)
			}
		}

		dbrs := make(map[string]resource.Resource)
		for _, r := range dbpp.Resources {
			dbrs[r.Canonical] = r
		}
		for _, r := range pp.Resources {
			if !utils.ValidateCanonical(r.Name) {
				return fmt.Errorf("invalid Resource Name format %q", r.Name)
			}
			spec := r.CheckInterval
			if spec == "" {
				spec = "@every 1m"
			}
			if err := scheduler.ValidateCheckInterval(spec); err != nil {
				return fmt.Errorf("invalid check_interval for resource %q: %w", r.Name, err)
			}
			if dbr, ok := dbrs[r.Canonical]; ok {
				delete(dbrs, r.Canonical)
				if dbr.CheckInterval != r.CheckInterval {
					nextCheck, err := scheduler.ComputeNextCheck(spec, time.Now())
					if err != nil {
						return fmt.Errorf("failed to compute next check for resource %q: %w", r.Canonical, err)
					}
					r.NextCheck = nextCheck
				} else {
					r.NextCheck = dbr.NextCheck
				}
				r.WebhookToken = dbr.WebhookToken
				err = uow.Resources().Update(ctx, tc, pCan, r.Canonical, r)
				if err != nil {
					return fmt.Errorf("failed to update Resource %q: %w", r.Canonical, err)
				}
			} else {
				nextCheck, err := scheduler.ComputeNextCheck(spec, time.Now())
				if err != nil {
					return fmt.Errorf("failed to compute next check for resource %q: %w", r.Canonical, err)
				}
				r.NextCheck = nextCheck
				r.WebhookToken, err = newWebhookToken(r.Canonical)
				if err != nil {
					return fmt.Errorf("failed to generate webhook token for resource %q: %w", r.Canonical, err)
				}
				_, err = uow.Resources().Create(ctx, tc, pCan, r)
				if err != nil {
					return fmt.Errorf("failed to create Resource %q: %w", r.Canonical, err)
				}
			}
		}
		for rc := range dbrs {
			err = uow.Resources().Delete(ctx, tc, pCan, rc)
			if err != nil {
				return fmt.Errorf("failed to delete Resource %q: %w", rc, err)
			}
		}

		dbru := make(map[string]struct{})
		for _, ru := range dbpp.Runners {
			dbru[ru.Name] = struct{}{}
		}
		for _, ru := range pp.Runners {
			if !utils.ValidateCanonical(ru.Name) {
				return fmt.Errorf("invalid Resource Name format %q", ru.Name)
			}
			if _, ok := dbru[ru.Name]; ok {
				delete(dbru, ru.Name)
				err = uow.Runners().Update(ctx, tc, pCan, ru.Name, ru)
				if err != nil {
					return fmt.Errorf("failed to update Runner %q: %w", ru.Name, err)
				}
			} else {
				_, err = uow.Runners().Create(ctx, tc, pCan, ru)
				if err != nil {
					return fmt.Errorf("failed to create Runner %q: %w", ru.Name, err)
				}
			}
		}
		for run := range dbru {
			err = uow.Runners().Delete(ctx, tc, pCan, run)
			if err != nil {
				return fmt.Errorf("failed to delete Runner %q: %w", run, err)
			}
		}

		dbsts := make(map[string]struct{})
		for _, st := range dbpp.SecretTypes {
			dbsts[st.Name] = struct{}{}
		}
		for _, st := range pp.SecretTypes {
			if !utils.ValidateCanonical(st.Name) {
				return fmt.Errorf("invalid SecretType Name format %q", st.Name)
			}
			if _, ok := dbsts[st.Name]; ok {
				delete(dbsts, st.Name)
				err = uow.SecretTypes().Update(ctx, tc, pCan, st.Name, st)
				if err != nil {
					return fmt.Errorf("failed to update SecretType %q: %w", st.Name, err)
				}
			} else {
				_, err = uow.SecretTypes().Create(ctx, tc, pCan, st)
				if err != nil {
					return fmt.Errorf("failed to create SecretType %q: %w", st.Name, err)
				}
			}
		}
		for stn := range dbsts {
			err = uow.SecretTypes().Delete(ctx, tc, pCan, stn)
			if err != nil {
				return fmt.Errorf("failed to delete SecretType %q: %w", stn, err)
			}
		}

		dbnts := make(map[string]struct{})
		for _, nt := range dbpp.NotificationTypes {
			dbnts[nt.Name] = struct{}{}
		}
		for _, nt := range pp.NotificationTypes {
			if !utils.ValidateCanonical(nt.Name) {
				return fmt.Errorf("invalid NotificationType Name format %q", nt.Name)
			}
			if _, ok := dbnts[nt.Name]; ok {
				delete(dbnts, nt.Name)
				err = uow.NotificationTypes().Update(ctx, tc, pCan, nt.Name, nt)
				if err != nil {
					return fmt.Errorf("failed to update NotificationType %q: %w", nt.Name, err)
				}
			} else {
				_, err = uow.NotificationTypes().Create(ctx, tc, pCan, nt)
				if err != nil {
					return fmt.Errorf("failed to create NotificationType %q: %w", nt.Name, err)
				}
			}
		}
		for ntn := range dbnts {
			err = uow.NotificationTypes().Delete(ctx, tc, pCan, ntn)
			if err != nil {
				return fmt.Errorf("failed to delete NotificationType %q: %w", ntn, err)
			}
		}

		dbns := make(map[string]notification.Notification)
		for _, n := range dbpp.Notifications {
			dbns[n.Canonical] = n
		}
		for _, n := range pp.Notifications {
			if !utils.ValidateCanonical(n.Name) {
				return fmt.Errorf("invalid Notification Name format %q", n.Name)
			}
			if _, ok := dbns[n.Canonical]; ok {
				delete(dbns, n.Canonical)
				err = uow.Notifications().Update(ctx, tc, pCan, n.Canonical, n)
				if err != nil {
					return fmt.Errorf("failed to update Notification %q: %w", n.Canonical, err)
				}
			} else {
				_, err = uow.Notifications().Create(ctx, tc, pCan, n)
				if err != nil {
					return fmt.Errorf("failed to create Notification %q: %w", n.Canonical, err)
				}
			}
		}
		for nc := range dbns {
			err = uow.Notifications().Delete(ctx, tc, pCan, nc)
			if err != nil {
				return fmt.Errorf("failed to delete Notification %q: %w", nc, err)
			}
		}

		up, err = uow.Pipelines().Find(ctx, tc, pCan)
		if err != nil {
			return fmt.Errorf("failed to get Pipeline: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	var details map[string]interface{}
	if up.Canonical != pCan {
		details = map[string]interface{}{"renamed_from": pCan}
	}
	q.audit(ctx, tc, auditlog.PipelineUpdated, "pipeline", up.Canonical, details)
	return up, nil
}

// ListPipelines returns all pipelines for the given team, enriched with the
// timestamp of the most recent build for each pipeline.
func (q *PikoCI) ListPipelines(ctx context.Context, tc string) ([]*pipeline.Pipeline, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	}

	pps, err := q.Pipelines.Filter(ctx, tc)
	if err != nil {
		return nil, fmt.Errorf("failed to filter Pipelines: %w", err)
	}

	lastBuilds, err := q.Builds.LastBuildAtByPipeline(ctx, tc)
	if err != nil {
		return nil, fmt.Errorf("failed to get last build timestamps: %w", err)
	}

	for _, pp := range pps {
		if t, ok := lastBuilds[pp.ID]; ok {
			t := t
			pp.LastBuildAt = &t
		}
	}

	return pps, nil
}

// GetPipeline retrieves a pipeline by team canonical and pipeline canonical.
func (q *PikoCI) GetPipeline(ctx context.Context, tc, pCan string) (*pipeline.Pipeline, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}

	pp, err := q.Pipelines.Find(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("failed to get Pipeline %q: %w", pCan, err)
	}

	return pp, nil
}

// jobColors and jobBorderColors map build statuses to PICO-8 palette hex
// colors used for rendering pipeline graph nodes.
var (
	jobColors = map[build.Status]string{
		build.Started:         `"#FFA300"`,
		build.Failed:          `"#FF004D"`,
		build.Succeeded:       `"#00A83A"`,
		build.Cancelled:       `"#AB5236"`,
		build.WaitingForApproval: `"#8e44ad"`,
	}
	jobBorderColors = map[build.Status]string{
		build.Started:         `"#CC8200"`,
		build.Failed:          `"#CC003E"`,
		build.Succeeded:       `"#008030"`,
		build.Cancelled:       `"#8A3F2B"`,
		build.WaitingForApproval: `"#6c3483"`,
	}
	colorResource       = `"#83769C"`
	colorResourceBorder = `"#5F574F"`
	colorDefault        = `"#83769C"`
	colorDefaultBorder  = `"#5F574F"`
	colorError          = `"#FF004D"`
	colorPaused         = `"#29ADFF"`
	colorPausedBorder   = `"#1D8BD1"`
	colorPinned         = `"#FFA300"`
)

// versionFilterOpts holds the parameters for filtering a pipeline graph to
// show only jobs in a specific version's path.
type versionFilterOpts struct {
	resourceCanonical string
	versionID         uint32
	chainJobs         map[string]bool
	buildsByJob       map[string][]*build.Build
}

// GetPipelineImage generates a DOT graph representation of a pipeline's jobs
// and resources, colored by the latest build status of each job.
func (q *PikoCI) GetPipelineImage(ctx context.Context, tc, pCan, format string, hideIntermediates, groupParallel bool, versionID *uint32) ([]byte, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}
	if format == "" {
		format = "dot"
	}
	if strings.Contains(format, ".") {
		format = strings.Split(format, ".")[1]
	}

	if format != "dot" && format != "svg" && format != "png" {
		return nil, fmt.Errorf("invalid image format %q", format)
	}

	pp, err := q.GetPipeline(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("failed to get Pipeline %q: %w", pCan, err)
	}

	var vf *versionFilterOpts
	if versionID != nil {
		vf, err = q.buildVersionFilter(ctx, tc, pCan, pp, *versionID)
		if err != nil {
			return nil, fmt.Errorf("failed to build version filter: %w", err)
		}
	}

	img, err := q.generateImage(ctx, tc, pp, hideIntermediates, groupParallel, vf)
	if err != nil {
		return nil, fmt.Errorf("failed to generate image: %w", err)
	}

	return convertDOTImage(ctx, img, format)
}

// buildVersionFilter resolves the version's chain and fetches builds,
// returning filter options for generateImage.
func (q *PikoCI) buildVersionFilter(ctx context.Context, tc, pn string, pp *pipeline.Pipeline, versionID uint32) (*versionFilterOpts, error) {
	// Look up which resource this version belongs to
	_, rCan, err := q.Resources.FindVersionByID(ctx, versionID)
	if err != nil {
		return nil, fmt.Errorf("version %d not found: %w", versionID, err)
	}

	chain := resolveResourceChain(pp.Jobs, rCan)
	chainSet := make(map[string]bool, len(chain))
	for _, jn := range chain {
		chainSet[jn] = true
	}

	buildsByJob, err := q.chainWalkBuilds(ctx, tc, pn, versionID, chain)
	if err != nil {
		return nil, fmt.Errorf("failed to find builds: %w", err)
	}

	return &versionFilterOpts{
		resourceCanonical: rCan,
		versionID:         versionID,
		chainJobs:         chainSet,
		buildsByJob:       buildsByJob,
	}, nil
}

// resolvedBuildStatus holds the latest completed and running builds for a job.
type resolvedBuildStatus struct {
	completedBuild *build.Build
	runningBuild   *build.Build
}

// resolveBuildStatus finds the latest completed build (including retries) and
// any running/pending build from a list of builds sorted newest-first.
func resolveBuildStatus(builds []*build.Build) resolvedBuildStatus {
	var cb, rb *build.Build
	for _, b := range builds {
		if b.Status == build.Started && (rb == nil || rb.Status == build.Pending) {
			rb = b
		} else if b.Status == build.Pending && rb == nil {
			rb = b
		}
	}
	// Find the latest terminal build by walking back through main build
	// groups until one with a completed (non-running, non-pending) build
	// is found.
	seen := map[string]bool{}
	for cb == nil {
		var mainBN string
		for _, b := range builds {
			if !strings.Contains(b.BuildNumber, ".") && !seen[b.BuildNumber] {
				mainBN = b.BuildNumber
				break
			}
		}
		if mainBN == "" {
			break
		}
		seen[mainBN] = true
		for _, b := range builds {
			if b.BuildNumber == mainBN || strings.HasPrefix(b.BuildNumber, mainBN+".") {
				if b.Status != build.Started && b.Status != build.Pending {
					cb = b
					break
				}
			}
		}
	}
	return resolvedBuildStatus{completedBuild: cb, runningBuild: rb}
}

// jobNodeVisuals holds the computed visual properties for rendering a job node
// in a DOT graph.
type jobNodeVisuals struct {
	color              string
	borderColor        string
	clusterStyle       string
	clusterBorderColor string
}

// resolveJobVisuals fetches builds for a job and computes its fill color,
// border color, and cluster (running indicator) style.
func (q *PikoCI) resolveJobVisuals(ctx context.Context, tc string, pp *pipeline.Pipeline, j job.Job) (jobNodeVisuals, error) {
	builds, err := q.Builds.Filter(ctx, tc, pp.Canonical, j.Name, nil, nil, 0)
	if err != nil {
		return jobNodeVisuals{}, fmt.Errorf("failed to filter builds from Job %q: %w", j.Name, err)
	}

	color := colorDefault
	borderColor := colorDefaultBorder

	bs := resolveBuildStatus(builds)

	if bs.completedBuild != nil {
		if c, ok := jobColors[bs.completedBuild.Status]; ok {
			color = c
		}
		if c, ok := jobBorderColors[bs.completedBuild.Status]; ok {
			borderColor = c
		}
	}

	if j.Paused {
		color = colorPaused
		borderColor = colorPausedBorder
	}

	clusterStyle := "invis"
	clusterBorderColor := jobBorderColors[build.Started]
	if bs.runningBuild != nil {
		clusterStyle = `"dashed,bold"`
		if bs.runningBuild.Status == build.Pending {
			clusterBorderColor = colorDefaultBorder
		}
	}

	return jobNodeVisuals{
		color:              color,
		borderColor:        borderColor,
		clusterStyle:       clusterStyle,
		clusterBorderColor: clusterBorderColor,
	}, nil
}

// resolveJobVisualsFromBuilds computes job node visuals from a provided set of
// builds rather than fetching from the database. Used when rendering a
// version-scoped graph where only specific builds are relevant.
func resolveJobVisualsFromBuilds(builds []*build.Build, j job.Job) jobNodeVisuals {
	color := colorDefault
	borderColor := colorDefaultBorder

	bs := resolveBuildStatus(builds)

	if bs.completedBuild != nil {
		if c, ok := jobColors[bs.completedBuild.Status]; ok {
			color = c
		}
		if c, ok := jobBorderColors[bs.completedBuild.Status]; ok {
			borderColor = c
		}
	}

	if j.Paused {
		color = colorPaused
		borderColor = colorPausedBorder
	}

	clusterStyle := "invis"
	clusterBorderColor := jobBorderColors[build.Started]
	if bs.runningBuild != nil {
		clusterStyle = `"dashed,bold"`
		if bs.runningBuild.Status == build.Pending {
			clusterBorderColor = colorDefaultBorder
		}
	}

	return jobNodeVisuals{
		color:              color,
		borderColor:        borderColor,
		clusterStyle:       clusterStyle,
		clusterBorderColor: clusterBorderColor,
	}
}

// generateImage builds a DOT-format directed graph representing the pipeline's
// jobs, resources, and their interconnections. Each job node is colored based on
// its latest build status, and running builds are highlighted with a dashed border.
// When hideIntermediates is true, intermediate resource nodes (between jobs via
// passed constraints and put outputs) are removed, keeping only entry-point
// trigger resources and drawing direct job-to-job edges.
func (q *PikoCI) generateImage(ctx context.Context, tc string, pp *pipeline.Pipeline, hideRes, groupParallel bool, versionFilter *versionFilterOpts) ([]byte, error) {
	var (
		pn  = fmt.Sprintf(`"%s"`, pp.Canonical)
		err error
	)

	graph := gographviz.NewGraph()
	graph.SetName(pn)
	graph.SetStrict(true)
	graph.AddAttr(pn, string(gographviz.RankDir), "LR")

	// When version filter is active, work with a local copy restricted to chain jobs
	if versionFilter != nil {
		var filteredJobs []job.Job
		for _, j := range pp.Jobs {
			if versionFilter.chainJobs[j.Name] {
				filteredJobs = append(filteredJobs, j)
			}
		}
		localPP := *pp
		localPP.Jobs = filteredJobs
		pp = &localPP
	}

	// Collect resources referenced by get steps without passed constraints.
	// Resources only accessed via get-with-passed don't need a standalone node
	// because the passed-edge logic creates its own intermediate resource nodes.
	referencedResources := make(map[string]bool)
	for _, j := range pp.Jobs {
		for _, g := range j.GetSteps() {
			if len(g.Passed) == 0 {
				referencedResources[g.ResourceCanonical()] = true
			}
		}
	}

	// Build tooltip text for each resource from its latest version.
	resourceTooltips := make(map[string]string)
	type resVersion struct {
		canonical string
		version   *resource.Version
	}
	var resVersions []resVersion
	var versionIDs []uint32
	for _, r := range pp.Resources {
		vers, err := q.Resources.FilterVersions(ctx, tc, pp.Canonical, r.Canonical, nil, nil, 1)
		if err != nil || len(vers) == 0 {
			continue
		}
		resVersions = append(resVersions, resVersion{canonical: r.Canonical, version: vers[0]})
		versionIDs = append(versionIDs, vers[0].ID)
	}
	if len(versionIDs) > 0 {
		statuses, err := q.Builds.AggregateStatusByVersionIDs(ctx, versionIDs)
		if err != nil {
			q.logger.Warn("failed to fetch aggregate version statuses for tooltips", "error", err)
		} else {
			for _, rv := range resVersions {
				if s, ok := statuses[rv.version.ID]; ok {
					rv.version.Status = s
				}
			}
		}
	}
	for _, rv := range resVersions {
		resourceTooltips[rv.canonical] = buildResourceTooltip(rv.version)
	}

	resourceBorders := make(map[string]string)
	resourceStyles := make(map[string]string)
	resourcePenwidths := make(map[string]string)
	// Print all the resources
	for _, r := range pp.Resources {
		borderColor := colorResourceBorder
		if r.Logs != "" {
			borderColor = colorError
		}
		style := "filled"
		penwidth := ""
		if r.PinnedVersionID != nil {
			borderColor = colorPinned
			style = `"filled,bold"`
			penwidth = "3.0"
		}
		resourceBorders[r.Canonical] = borderColor
		resourceStyles[r.Canonical] = style
		resourcePenwidths[r.Canonical] = penwidth
		if !referencedResources[r.Canonical] {
			continue
		}
		vurl := fmt.Sprintf(`"/teams/%s/pipelines/%s/resources/%s/versions"`, tc, pp.Canonical, r.Canonical)
		attrs := map[string]string{
			string(gographviz.Margin):    "0.2",
			string(gographviz.Shape):     "cds",
			string(gographviz.FillColor): colorResource,
			string(gographviz.Style):     style,
			string(gographviz.FontColor): "white",
			string(gographviz.URL):       vurl,
			string(gographviz.Color):     borderColor,
		}
		if penwidth != "" {
			attrs["penwidth"] = penwidth
		}
		if tip, ok := resourceTooltips[r.Canonical]; ok {
			attrs[string(gographviz.Tooltip)] = fmt.Sprintf(`"%s"`, tip)
		}
		err = graph.AddNode(pn, fmt.Sprintf(`"%s"`, r.Canonical), attrs)
		if err != nil {
			return nil, fmt.Errorf("failed to add node to Graph: %w", err)
		}
	}

	// Build a map of for_each group names to instance names for passed expansion
	forEachGroupInstances := make(map[string][]string)
	for _, j := range pp.Jobs {
		if j.ForEachGroup != "" {
			forEachGroupInstances[j.ForEachGroup] = append(forEachGroupInstances[j.ForEachGroup], j.Name)
		}
	}

	// expandPassed expands for_each group names to instance names in a passed list.
	expandPassed := func(passed []string) []string {
		var expanded []string
		for _, name := range passed {
			if instances, ok := forEachGroupInstances[name]; ok {
				expanded = append(expanded, instances...)
			} else {
				expanded = append(expanded, name)
			}
		}
		return expanded
	}

	// Detect parallel groups: jobs sharing identical expanded passed parent sets.
	// Only groups with 2+ members qualify. Root jobs (no parents) are never grouped.
	jobToGroupNode := make(map[string]string) // jobName → group node name
	type parallelGroup struct {
		nodeName string
		jobs     []job.Job
	}
	var parallelGroups []parallelGroup
	if groupParallel {
		keyToJobs := make(map[string][]job.Job)
		var keyOrder []string
		for _, j := range pp.Jobs {
			var allPassed []string
			for _, g := range j.GetSteps() {
				if len(g.Passed) > 0 {
					allPassed = append(allPassed, expandPassed(g.Passed)...)
				}
			}
			var key string
			if len(allPassed) == 0 {
				// Root jobs: group by their trigger resource set
				var triggerResources []string
				for _, g := range j.GetSteps() {
					if len(g.Passed) == 0 {
						triggerResources = append(triggerResources, g.ResourceCanonical())
					}
				}
				if len(triggerResources) == 0 {
					continue
				}
				sort.Strings(triggerResources)
				key = "root:" + strings.Join(triggerResources, ",")
			} else {
				sort.Strings(allPassed)
				// Deduplicate
				deduped := allPassed[:0]
				for i, p := range allPassed {
					if i == 0 || p != allPassed[i-1] {
						deduped = append(deduped, p)
					}
				}
				key = strings.Join(deduped, ",")
			}
			if _, ok := keyToJobs[key]; !ok {
				keyOrder = append(keyOrder, key)
			}
			keyToJobs[key] = append(keyToJobs[key], j)
		}
		for gi, key := range keyOrder {
			jobs := keyToJobs[key]
			if len(jobs) < 2 {
				continue
			}
			gn := fmt.Sprintf(`"parallel_%d"`, gi)
			pg := parallelGroup{nodeName: gn, jobs: jobs}
			parallelGroups = append(parallelGroups, pg)
			for _, j := range jobs {
				jobToGroupNode[j.Name] = gn
			}
		}
	}

	// resolveNodeName returns the graph node name for a job, remapping to group node if grouped.
	resolveNodeName := func(jobName string) string {
		if gn, ok := jobToGroupNode[jobName]; ok {
			return gn
		}
		return fmt.Sprintf(`"%s"`, jobName)
	}

	// Print all the Jobs and the connection to resources
	for i, j := range pp.Jobs {
		if _, grouped := jobToGroupNode[j.Name]; grouped {
			continue // grouped jobs are rendered as part of their parallel group node
		}

		var vis jobNodeVisuals
		if versionFilter != nil {
			// Use only the version-specific builds for coloring
			vis = resolveJobVisualsFromBuilds(versionFilter.buildsByJob[j.Name], j)
		} else {
			var verr error
			vis, verr = q.resolveJobVisuals(ctx, tc, pp, j)
			if verr != nil {
				return nil, verr
			}
		}

		jg := fmt.Sprintf("cluster_%d", i)
		graph.AddSubGraph(pn, jg, map[string]string{
			string(gographviz.Style): vis.clusterStyle,
			string(gographviz.Color): vis.clusterBorderColor,
		})

		burl := fmt.Sprintf(`"/teams/%s/pipelines/%s/jobs/%s/builds"`, tc, pp.Canonical, j.Name)
		quotedJobName := fmt.Sprintf(`"%s"`, j.Name)
		err = graph.AddNode(jg, quotedJobName, map[string]string{
			string(gographviz.Margin):    "0.5",
			string(gographviz.Shape):     "rectangle",
			string(gographviz.FillColor): vis.color,
			string(gographviz.Style):     "filled",
			string(gographviz.FontColor): "white",
			string(gographviz.Color):     vis.borderColor,
			string(gographviz.URL):       burl,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add node to Graph: %w", err)
		}
		// Draw resource→job edges for get steps without passed constraints
		for _, g := range j.GetSteps() {
			if len(g.Passed) == 0 {
				rCan := fmt.Sprintf(`"%s.%s"`, g.Type, g.Name)
				opt := make(map[string]string)
				if g.Trigger {
					opt[string(gographviz.Style)] = "solid"
				} else {
					opt[string(gographviz.Style)] = "dashed"
				}
				err = graph.AddEdge(rCan, quotedJobName, false, opt)
				if err != nil {
					return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
				}
			}
		}
		// Draw job→resource edges for all put steps (plan + hooks).
		// Each job gets its own output resource node to avoid all jobs
		// pointing to a single shared resource box.
		// Skipped when hiding intermediate resources.
		if !hideRes {
			for _, p := range j.AllPutSteps() {
				rCan := fmt.Sprintf("%s.%s", p.Type, p.Name)
				nn := fmt.Sprintf(`"%s-%s-out"`, j.Name, rCan)
				vurl := fmt.Sprintf(`"/teams/%s/pipelines/%s/resources/%s/versions"`, tc, pp.Canonical, rCan)
				border := resourceBorders[rCan]
				rStyle := resourceStyles[rCan]
				if rStyle == "" {
					rStyle = "filled"
				}
				putAttrs := map[string]string{
					string(gographviz.Label):     fmt.Sprintf(`"%s"`, rCan),
					string(gographviz.Margin):    "0.2",
					string(gographviz.Shape):     "cds",
					string(gographviz.FillColor): colorResource,
					string(gographviz.Style):     rStyle,
					string(gographviz.FontColor): "white",
					string(gographviz.URL):       vurl,
					string(gographviz.Color):     border,
				}
				if pw := resourcePenwidths[rCan]; pw != "" {
					putAttrs["penwidth"] = pw
				}
				if tip, ok := resourceTooltips[rCan]; ok {
					putAttrs[string(gographviz.Tooltip)] = fmt.Sprintf(`"%s"`, tip)
				}
				err = graph.AddNode(pn, nn, putAttrs)
				if err != nil {
					return nil, fmt.Errorf("failed to add node to Graph: %w", err)
				}
				err = graph.AddEdge(quotedJobName, nn, false, map[string]string{
					string(gographviz.Style): "solid",
				})
				if err != nil {
					return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
				}
			}
		}
	}

	// Render parallel group nodes as HTML label tables
	for _, pg := range parallelGroups {
		// Determine worst status for border color
		statusPriority := map[build.Status]int{
			build.Failed:    4,
			build.Started:   3,
			build.Cancelled: 2,
			build.Succeeded: 1,
		}
		worstPriority := 0
		var worstStatus build.Status
		var rows []string
		for _, j := range pg.jobs {
			var vis jobNodeVisuals
			var jobBuilds []*build.Build
			if versionFilter != nil {
				vis = resolveJobVisualsFromBuilds(versionFilter.buildsByJob[j.Name], j)
				jobBuilds = versionFilter.buildsByJob[j.Name]
			} else {
				var verr error
				vis, verr = q.resolveJobVisuals(ctx, tc, pp, j)
				if verr != nil {
					return nil, verr
				}
				jobBuilds, _ = q.Builds.Filter(ctx, tc, pp.Canonical, j.Name, nil, nil, 0)
			}
			dotColor := strings.Trim(vis.color, `"`)
			if len(jobBuilds) > 0 {
				bs := resolveBuildStatus(jobBuilds)
				if bs.completedBuild != nil {
					if p, ok := statusPriority[bs.completedBuild.Status]; ok && p > worstPriority {
						worstPriority = p
						worstStatus = bs.completedBuild.Status
					}
				}
				if bs.runningBuild != nil {
					if c, ok := jobColors[bs.runningBuild.Status]; ok {
						dotColor = strings.Trim(c, `"`)
					}
					if p, ok := statusPriority[bs.runningBuild.Status]; ok && p > worstPriority {
						worstPriority = p
						worstStatus = bs.runningBuild.Status
					}
				}
			}
			if j.Paused {
				dotColor = strings.Trim(colorPaused, `"`)
			}
			burl := fmt.Sprintf("/teams/%s/pipelines/%s/jobs/%s/builds", tc, pp.Canonical, j.Name)
			row := fmt.Sprintf(
				`<TR><TD BGCOLOR="%s" WIDTH="12" HEIGHT="12"> </TD><TD ALIGN="LEFT" HREF="%s"><FONT COLOR="white"> %s </FONT></TD></TR>`,
				dotColor, burl, j.Name,
			)
			rows = append(rows, row)
		}
		borderColor := strings.Trim(colorDefaultBorder, `"`)
		if worstPriority > 0 {
			if c, ok := jobBorderColors[worstStatus]; ok {
				borderColor = strings.Trim(c, `"`)
			}
		}
		header := fmt.Sprintf(
			`<TR><TD COLSPAN="2" BGCOLOR="#1D2B53" ALIGN="CENTER"><FONT COLOR="white"><B>PARALLEL (%d JOBS)</B></FONT></TD></TR>`,
			len(pg.jobs),
		)
		htmlLabel := fmt.Sprintf(
			`<<TABLE BORDER="1" CELLBORDER="0" CELLSPACING="4" CELLPADDING="6" COLOR="%s" BGCOLOR="#1D2B53" STYLE="ROUNDED">%s%s</TABLE>>`,
			borderColor, header, strings.Join(rows, ""),
		)
		err = graph.AddNode(pn, pg.nodeName, map[string]string{
			string(gographviz.Shape): "plaintext",
			string(gographviz.Label): htmlLabel,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add parallel group node to Graph: %w", err)
		}

		// Draw resource→group edges for get steps without passed constraints
		for _, j := range pg.jobs {
			for _, g := range j.GetSteps() {
				if len(g.Passed) == 0 {
					rCan := fmt.Sprintf(`"%s.%s"`, g.Type, g.Name)
					opt := make(map[string]string)
					if g.Trigger {
						opt[string(gographviz.Style)] = "solid"
					} else {
						opt[string(gographviz.Style)] = "dashed"
					}
					err = graph.AddEdge(rCan, pg.nodeName, false, opt)
					if err != nil {
						return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
					}
				}
			}
		}

		// Draw group→resource edges for put steps (when not hiding intermediates)
		if !hideRes {
			for _, j := range pg.jobs {
				for _, p := range j.AllPutSteps() {
					rCan := fmt.Sprintf("%s.%s", p.Type, p.Name)
					nn := fmt.Sprintf(`"%s-%s-out"`, j.Name, rCan)
					vurl := fmt.Sprintf(`"/teams/%s/pipelines/%s/resources/%s/versions"`, tc, pp.Canonical, rCan)
					border := resourceBorders[rCan]
					rStyle := resourceStyles[rCan]
					if rStyle == "" {
						rStyle = "filled"
					}
					putAttrs := map[string]string{
						string(gographviz.Label):     fmt.Sprintf(`"%s"`, rCan),
						string(gographviz.Margin):    "0.2",
						string(gographviz.Shape):     "cds",
						string(gographviz.FillColor): colorResource,
						string(gographviz.Style):     rStyle,
						string(gographviz.FontColor): "white",
						string(gographviz.URL):       vurl,
						string(gographviz.Color):     border,
					}
					if pw := resourcePenwidths[rCan]; pw != "" {
						putAttrs["penwidth"] = pw
					}
					if tip, ok := resourceTooltips[rCan]; ok {
						putAttrs[string(gographviz.Tooltip)] = fmt.Sprintf(`"%s"`, tip)
					}
					err = graph.AddNode(pn, nn, putAttrs)
					if err != nil {
						return nil, fmt.Errorf("failed to add node to Graph: %w", err)
					}
					err = graph.AddEdge(pg.nodeName, nn, false, map[string]string{
						string(gographviz.Style): "solid",
					})
					if err != nil {
						return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
					}
				}
			}
		}
	}

	// Collect put output node names per job+resource so passed edges can reuse them
	putOutputNodes := make(map[string]string) // key: "jobName-type.name" → node name
	for _, j := range pp.Jobs {
		for _, p := range j.AllPutSteps() {
			rCan := fmt.Sprintf("%s.%s", p.Type, p.Name)
			key := fmt.Sprintf("%s-%s", j.Name, rCan)
			putOutputNodes[key] = fmt.Sprintf(`"%s-%s-out"`, j.Name, rCan)
		}
	}

	// Now we print all the jobs interconnections depending on resources
	for _, j := range pp.Jobs {
		quotedJobName := resolveNodeName(j.Name)
		for _, g := range j.GetSteps() {
			if len(g.Passed) != 0 {
				expandedPassed := expandPassed(g.Passed)
				if hideRes {
					// Direct job-to-job edges when hiding intermediates
					for _, p := range expandedPassed {
						quotedPassedName := resolveNodeName(p)
						err = graph.AddEdge(quotedPassedName, quotedJobName, false, nil)
						if err != nil {
							return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
						}
					}
				} else {
					for _, p := range expandedPassed {
						rCan := fmt.Sprintf("%s.%s", g.Type, g.Name)

						// Reuse the put output node if the passed job already has one for this resource
						key := fmt.Sprintf("%s-%s", p, rCan)
						if nn, ok := putOutputNodes[key]; ok {
							err = graph.AddEdge(nn, quotedJobName, false, nil)
							if err != nil {
								return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
							}
							continue
						}

						nn := fmt.Sprintf(`"%s-%s-%s"`, p, g.Name, j.Name)
						vurl := fmt.Sprintf(`"/teams/%s/pipelines/%s/resources/%s/versions"`, tc, pp.Canonical, rCan)
						border := resourceBorders[rCan]
						rStyle := resourceStyles[rCan]
						if rStyle == "" {
							rStyle = "filled"
						}
						passedAttrs := map[string]string{
							string(gographviz.Label):     fmt.Sprintf(`"%s"`, rCan),
							string(gographviz.Margin):    "0.2",
							string(gographviz.Shape):     "cds",
							string(gographviz.FillColor): colorResource,
							string(gographviz.Style):     rStyle,
							string(gographviz.FontColor): "white",
							string(gographviz.URL):       vurl,
							string(gographviz.Color):     border,
						}
						if pw := resourcePenwidths[rCan]; pw != "" {
							passedAttrs["penwidth"] = pw
						}
						if tip, ok := resourceTooltips[rCan]; ok {
							passedAttrs[string(gographviz.Tooltip)] = fmt.Sprintf(`"%s"`, tip)
						}
						quotedPassedName := resolveNodeName(p)
						err = graph.AddNode(pn, nn, passedAttrs)
						if err != nil {
							return nil, fmt.Errorf("failed to add node to Graph: %w", err)
						}
						err = graph.AddEdge(quotedPassedName, nn, false, nil)
						if err != nil {
							return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
						}
						err = graph.AddEdge(nn, quotedJobName, false, nil)
						if err != nil {
							return nil, fmt.Errorf("failed to add edge to Graph: %w", err)
						}
					}
				}
			}
		}
	}

	str := graph.String()
	return []byte(str), nil
}

// buildResourceTooltip formats a resource version's key-value pairs and
// aggregate build status into a newline-separated tooltip string.
func buildResourceTooltip(v *resource.Version) string {
	keys := make([]string, 0, len(v.Version))
	for k := range v.Version {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var lines []string
	for _, k := range keys {
		val := fmt.Sprintf("%v", v.Version[k])
		val = strings.ReplaceAll(val, `\`, `\\`)
		val = strings.ReplaceAll(val, `"`, `\"`)
		lines = append(lines, fmt.Sprintf("%s: %s", k, val))
	}
	if v.Status != "" {
		lines = append(lines, v.Status)
	}
	return strings.Join(lines, "\\n")
}

// convertDOTImage converts raw DOT bytes to the requested format.
// For "dot" it returns the input unchanged. For "svg" or "png" it shells out
// to graphviz and (for SVG) applies post-processing.
func convertDOTImage(ctx context.Context, dot []byte, format string) ([]byte, error) {
	if format == "dot" {
		return dot, nil
	}
	img, err := renderDOT(ctx, dot, format)
	if err != nil {
		return nil, fmt.Errorf("failed to render DOT to %s: %w", format, err)
	}
	if format == "svg" {
		img, err = postProcessSVG(img)
		if err != nil {
			return nil, fmt.Errorf("failed to post-process SVG: %w", err)
		}
	}
	return img, nil
}

// renderDOT converts DOT graph bytes to the specified format (svg or png)
// by shelling out to the graphviz `dot` command with a 30-second timeout.
func renderDOT(ctx context.Context, dot []byte, format string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dot", "-T"+format)
	cmd.Stdin = bytes.NewReader(dot)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("dot command failed: %s: %w", stderr.String(), err)
	}
	return stdout.Bytes(), nil
}

// Precompiled regexes for SVG post-processing.
var (
	svgStyleRe  = regexp.MustCompile(`(<svg[^>]*style=")`)
	textRe      = regexp.MustCompile(`(<text\b)`)
	nodeGroupRe = regexp.MustCompile(`(<g[^>]*class="node"[^>]*>)`)
	polygonRe   = regexp.MustCompile(`<polygon\b([^>]*)/>`)
	rectRe      = regexp.MustCompile(`<rect\b([^>]*)/>`)
)

// postProcessSVG applies styling transforms to match the frontend rendering:
// transparent background, rounded rectangles, custom font, and pointer cursor on nodes.
func postProcessSVG(svg []byte) ([]byte, error) {
	s := string(svg)

	// Set transparent background on root <svg> element
	s = svgStyleRe.ReplaceAllString(s, `${1}background: transparent; `)
	if !strings.Contains(s, `style="background: transparent`) {
		s = strings.Replace(s, "<svg ", `<svg style="background: transparent" `, 1)
	}

	// Process polygon and rect elements: make background transparent, convert filled polygons to rounded rects
	firstBgDone := false
	s = processPolygonsAndRects(s, &firstBgDone)

	// Set font-family on all <text> elements
	s = textRe.ReplaceAllString(s, `<text style="font-family: 'Plus Jakarta Sans', system-ui, sans-serif"`)
	// Fix double <text attributes if text already had style
	s = strings.ReplaceAll(s, `<text style="font-family: 'Plus Jakarta Sans', system-ui, sans-serif" style="`, `<text style="font-family: 'Plus Jakarta Sans', system-ui, sans-serif; `)

	// Set cursor: pointer on g.node elements
	s = nodeGroupRe.ReplaceAllStringFunc(s, func(match string) string {
		if strings.Contains(match, "style=") {
			return strings.Replace(match, `style="`, `style="cursor: pointer; `, 1)
		}
		return strings.Replace(match, ">", ` style="cursor: pointer">`, 1)
	})

	return []byte(s), nil
}

// processPolygonsAndRects processes SVG polygon/rect elements to:
// 1. Make the first polygon/rect (background) transparent
// 2. Convert filled polygons that are axis-aligned rectangles into <rect> with rounded corners
func processPolygonsAndRects(s string, firstBgDone *bool) string {
	// Process polygons
	s = polygonRe.ReplaceAllStringFunc(s, func(match string) string {
		fill := extractAttr(match, "fill")

		// First polygon or white-filled: make transparent (background)
		if !*firstBgDone || strings.EqualFold(fill, "white") || strings.EqualFold(fill, "#ffffff") {
			*firstBgDone = true
			match = setAttr(match, "fill", "transparent")
			match = setAttr(match, "stroke", "transparent")
			return match
		}

		// Filled polygons (not none/transparent): convert to rounded rect if axis-aligned
		if fill != "" && !strings.EqualFold(fill, "none") && !strings.EqualFold(fill, "transparent") {
			points := extractAttr(match, "points")
			if rect, ok := polygonToRoundedRect(points, match); ok {
				return rect
			}
		}
		return match
	})

	// Process rects similarly for the first-bg rule
	s = rectRe.ReplaceAllStringFunc(s, func(match string) string {
		fill := extractAttr(match, "fill")
		if !*firstBgDone || strings.EqualFold(fill, "white") || strings.EqualFold(fill, "#ffffff") {
			*firstBgDone = true
			match = setAttr(match, "fill", "transparent")
			match = setAttr(match, "stroke", "transparent")
		}
		return match
	})

	return s
}

// polygonToRoundedRect converts a polygon with 4-5 points forming an axis-aligned
// rectangle into a <rect> element with rounded corners (rx=4, ry=4).
func polygonToRoundedRect(points string, original string) (string, bool) {
	if points == "" {
		return "", false
	}
	parts := strings.Fields(strings.TrimSpace(points))
	if len(parts) != 4 && len(parts) != 5 {
		return "", false
	}

	type pt struct{ x, y float64 }
	pts := make([]pt, len(parts))
	for i, p := range parts {
		xy := strings.Split(p, ",")
		if len(xy) != 2 {
			return "", false
		}
		x, err := strconv.ParseFloat(xy[0], 64)
		if err != nil {
			return "", false
		}
		y, err := strconv.ParseFloat(xy[1], 64)
		if err != nil {
			return "", false
		}
		pts[i] = pt{x, y}
	}

	// If 5 points, last must equal first (closed polygon)
	if len(pts) == 5 && (pts[0].x != pts[4].x || pts[0].y != pts[4].y) {
		return "", false
	}

	minX, maxX := pts[0].x, pts[0].x
	minY, maxY := pts[0].y, pts[0].y
	for _, p := range pts[:4] {
		minX = math.Min(minX, p.x)
		maxX = math.Max(maxX, p.x)
		minY = math.Min(minY, p.y)
		maxY = math.Max(maxY, p.y)
	}

	fill := extractAttr(original, "fill")
	stroke := extractAttr(original, "stroke")
	if stroke == "" {
		stroke = "none"
	}

	return fmt.Sprintf(`<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="4" ry="4" fill="%s" stroke="%s"/>`,
		minX, minY, maxX-minX, maxY-minY, fill, stroke), true
}

// extractAttr extracts the value of an XML attribute from a tag string.
func extractAttr(tag, name string) string {
	prefix := name + `="`
	idx := strings.Index(tag, prefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(prefix)
	end := strings.Index(tag[start:], `"`)
	if end < 0 {
		return ""
	}
	return tag[start : start+end]
}

// setAttr sets or replaces an XML attribute value in a tag string.
func setAttr(tag, name, value string) string {
	prefix := name + `="`
	idx := strings.Index(tag, prefix)
	if idx >= 0 {
		start := idx + len(prefix)
		end := strings.Index(tag[start:], `"`)
		if end >= 0 {
			return tag[:idx] + fmt.Sprintf(`%s="%s"`, name, value) + tag[start+end+1:]
		}
	}
	return strings.Replace(tag, "/>", fmt.Sprintf(` %s="%s"/>`, name, value), 1)
}

// CreatePipelineImage generates a DOT graph image from raw pipeline configuration
// bytes without persisting the pipeline. This is useful for previewing a pipeline
// layout before creating it.
func (q *PikoCI) CreatePipelineImage(ctx context.Context, tc string, pipeline []byte, vars map[string]interface{}, format string) ([]byte, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	}

	pp, err := ReadPipeline(ctx, pipeline, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to read Pipeline: %w", err)
	}

	pp.Name = "pikoci"
	pp.Canonical = "pikoci"

	if format == "" {
		format = "dot"
	}
	if strings.Contains(format, ".") {
		format = strings.Split(format, ".")[1]
	}
	if format != "dot" && format != "svg" && format != "png" {
		return nil, fmt.Errorf("invalid image format %q", format)
	}

	img, err := q.generateImage(ctx, tc, pp, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate image: %w", err)
	}

	return convertDOTImage(ctx, img, format)
}

// sanitizePipelineForPublic returns a copy of the pipeline with sensitive fields
// (raw config, resource params, webhook tokens) removed for public consumption.
func sanitizePipelineForPublic(pp *pipeline.Pipeline) *pipeline.Pipeline {
	cp := *pp
	cp.Raw = nil
	rs := make([]resource.Resource, len(cp.Resources))
	for i, r := range cp.Resources {
		rs[i] = sanitizeResourceForPublic(r)
	}
	cp.Resources = rs
	rts := make([]restype.ResourceType, len(cp.ResourceTypes))
	for i, rt := range cp.ResourceTypes {
		rts[i] = restype.ResourceType{
			ID:     rt.ID,
			Name:   rt.Name,
			Source: rt.Source,
		}
	}
	cp.ResourceTypes = rts
	sts := make([]sectype.SecretType, len(cp.SecretTypes))
	for i, st := range cp.SecretTypes {
		sts[i] = sectype.SecretType{
			ID:     st.ID,
			Name:   st.Name,
			Source: st.Source,
		}
	}
	cp.SecretTypes = sts
	nts := make([]notiftype.NotificationType, len(cp.NotificationTypes))
	for i, nt := range cp.NotificationTypes {
		nts[i] = notiftype.NotificationType{
			ID:     nt.ID,
			Name:   nt.Name,
			Source: nt.Source,
		}
	}
	cp.NotificationTypes = nts
	ns := make([]notification.Notification, len(cp.Notifications))
	for i, n := range cp.Notifications {
		ns[i] = notification.Notification{
			ID:        n.ID,
			Type:      n.Type,
			Name:      n.Name,
			Canonical: n.Canonical,
			On:        n.On,
			Jobs:      n.Jobs,
			Exclude:   n.Exclude,
		}
	}
	cp.Notifications = ns
	return &cp
}

// sanitizeResourceForPublic removes sensitive fields (params, webhook token, logs)
// from a resource for public access.
func sanitizeResourceForPublic(r resource.Resource) resource.Resource {
	r.Params = nil
	r.WebhookToken = ""
	r.Logs = ""
	return r
}

// SetPipelinePublic toggles the public visibility of a pipeline.
func (q *PikoCI) SetPipelinePublic(ctx context.Context, tc, pCan string, public bool) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}

	return q.Pipelines.SetPublic(ctx, tc, pCan, public)
}

// PausePipeline pauses all jobs in a pipeline.
func (q *PikoCI) PausePipeline(ctx context.Context, tc, pCan string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}
	err := q.Jobs.PauseAll(ctx, tc, pCan)
	if err != nil {
		return err
	}
	q.audit(ctx, tc, auditlog.PipelinePaused, "pipeline", pCan, nil)
	return nil
}

// UnpausePipeline unpauses all jobs in a pipeline.
func (q *PikoCI) UnpausePipeline(ctx context.Context, tc, pCan string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}
	err := q.Jobs.UnpauseAll(ctx, tc, pCan)
	if err != nil {
		return err
	}
	q.audit(ctx, tc, auditlog.PipelineUnpaused, "pipeline", pCan, nil)
	return nil
}

// GetPublicPipeline retrieves a public pipeline with sensitive fields sanitized.
// It returns an error if the pipeline does not exist or is not marked as public.
func (q *PikoCI) GetPublicPipeline(ctx context.Context, tc, pCan string) (*pipeline.Pipeline, error) {
	pp, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}
	return sanitizePipelineForPublic(pp), nil
}

// GetPublicPipelineImage generates a DOT graph image for a public pipeline.
func (q *PikoCI) GetPublicPipelineImage(ctx context.Context, tc, pCan, format string, hideIntermediates, groupParallel bool, versionID *uint32) ([]byte, error) {
	pp, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	if format == "" {
		format = "dot"
	}
	if strings.Contains(format, ".") {
		format = strings.Split(format, ".")[1]
	}
	if format != "dot" && format != "svg" && format != "png" {
		return nil, fmt.Errorf("invalid image format %q", format)
	}

	var vf *versionFilterOpts
	if versionID != nil {
		vf, err = q.buildVersionFilter(ctx, tc, pCan, pp, *versionID)
		if err != nil {
			return nil, fmt.Errorf("failed to build version filter: %w", err)
		}
	}

	img, err := q.generateImage(ctx, tc, pp, hideIntermediates, groupParallel, vf)
	if err != nil {
		return nil, err
	}

	return convertDOTImage(ctx, img, format)
}

// GetPublicPipelineJob retrieves a job from a public pipeline.
func (q *PikoCI) GetPublicPipelineJob(ctx context.Context, tc, pCan, jn string) (*job.Job, error) {
	_, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	return q.GetPipelineJob(ctx, tc, pCan, jn)
}

// ListPublicJobBuilds returns paginated builds for a job on a public pipeline,
// with secret step logs redacted for safety.
func (q *PikoCI) ListPublicJobBuilds(ctx context.Context, tc, pCan, jn string, before *uint32, after *uint32, limit uint32) ([]*build.Build, bool, error) {
	_, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, false, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	builds, hasMore, err := q.ListJobBuilds(ctx, tc, pCan, jn, before, after, limit)
	if err != nil {
		return nil, false, err
	}
	for _, b := range builds {
		for i, s := range b.Steps {
			if s.Type == "secret" {
				b.Steps[i].Logs = ""
			}
		}
	}
	return builds, hasMore, nil
}

// ListPublicPipelineResources returns all resources for a public pipeline with
// sensitive fields sanitized.
func (q *PikoCI) ListPublicPipelineResources(ctx context.Context, tc, pCan string) ([]*resource.Resource, error) {
	_, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	rs, err := q.ListPipelineResources(ctx, tc, pCan)
	if err != nil {
		return nil, err
	}

	for i, r := range rs {
		sr := sanitizeResourceForPublic(*r)
		rs[i] = &sr
	}

	return rs, nil
}

// GetPublicPipelineResource retrieves a resource from a public pipeline with
// sensitive fields sanitized.
func (q *PikoCI) GetPublicPipelineResource(ctx context.Context, tc, pCan, rCan string) (*resource.Resource, error) {
	_, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	r, err := q.GetPipelineResource(ctx, tc, pCan, rCan)
	if err != nil {
		return nil, err
	}

	sr := sanitizeResourceForPublic(*r)
	return &sr, nil
}

// ListPublicResourceVersions returns paginated resource versions for a public pipeline.
func (q *PikoCI) ListPublicResourceVersions(ctx context.Context, tc, pCan, rCan string, before *uint32, after *uint32, limit uint32) ([]*resource.Version, bool, error) {
	_, err := q.Pipelines.FindPublic(ctx, tc, pCan)
	if err != nil {
		return nil, false, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	return q.ListResourceVersions(ctx, tc, pCan, rCan, before, after, limit)
}

// DeletePipeline removes a pipeline and all its associated entities within a
// single unit of work.
func (q *PikoCI) DeletePipeline(ctx context.Context, tc, pCan string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pCan) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pCan)
	}

	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		err := uow.Pipelines().Delete(ctx, tc, pCan)
		if err != nil {
			return fmt.Errorf("failed to delete Pipeline %q: %w", pCan, err)
		}

		return nil
	})
	if err != nil {
		return err
	}
	q.audit(ctx, tc, auditlog.PipelineDeleted, "pipeline", pCan, nil)
	return nil
}
