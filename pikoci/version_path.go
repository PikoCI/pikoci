package pikoci

import (
	"context"
	"fmt"

	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/utils"
)

// resolveResourceChain performs a BFS starting from jobs that get the given
// resource without passed constraints, then follows downstream jobs via passed
// constraints. It returns the ordered list of job names in the chain.
func resolveResourceChain(jobs []job.Job, resourceCanonical string) []string {
	// Build for_each group expansion map
	forEachGroupInstances := make(map[string][]string)
	for _, j := range jobs {
		if j.ForEachGroup != "" {
			forEachGroupInstances[j.ForEachGroup] = append(forEachGroupInstances[j.ForEachGroup], j.Name)
		}
	}

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

	// Step 1: find entry-point jobs that get this resource directly (no passed)
	visited := make(map[string]bool)
	var queue []string
	var chain []string

	for _, j := range jobs {
		for _, g := range j.GetSteps() {
			if g.ResourceCanonical() == resourceCanonical && len(g.Passed) == 0 {
				if !visited[j.Name] {
					visited[j.Name] = true
					queue = append(queue, j.Name)
				}
				break
			}
		}
	}

	// Step 2: BFS through downstream jobs via passed constraints
	for len(queue) > 0 {
		jobName := queue[0]
		queue = queue[1:]
		chain = append(chain, jobName)

		for _, j := range jobs {
			if visited[j.Name] {
				continue
			}
			for _, g := range j.GetSteps() {
				if len(g.Passed) == 0 {
					continue
				}
				expandedPassed := expandPassed(g.Passed)
				for _, p := range expandedPassed {
					if p == jobName {
						visited[j.Name] = true
						queue = append(queue, j.Name)
						break
					}
				}
				if visited[j.Name] {
					break
				}
			}
		}
	}

	return chain
}

// resolveVersionPath builds the version path response for a pipeline, resource,
// and version. It resolves the chain, fetches builds, and assembles the response.
func (q *PikoCI) resolveVersionPath(ctx context.Context, tc, pn, rCan string, pp *pipeline.Pipeline, versionID uint32, redactLogs bool) (*resource.VersionPathResponse, error) {
	// Verify resource exists in pipeline
	var found bool
	for _, r := range pp.Resources {
		if r.Canonical == rCan {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("resource %q not found in pipeline %q", rCan, pn)
	}

	// Get the version by ID and verify it belongs to the specified resource
	version, versionResource, err := q.Resources.FindVersionByID(ctx, versionID)
	if err != nil {
		return nil, fmt.Errorf("version %d not found: %w", versionID, err)
	}
	if versionResource != rCan {
		return nil, fmt.Errorf("version %d belongs to resource %q, not %q", versionID, versionResource, rCan)
	}

	// Resolve chain
	chain := resolveResourceChain(pp.Jobs, rCan)
	if len(chain) == 0 {
		return &resource.VersionPathResponse{
			Resource: resource.VersionPathResource{
				Canonical: rCan,
				Version:   version.Version,
			},
			Path:      []resource.VersionPathEntry{},
			Completed: 0,
			Total:     0,
		}, nil
	}

	// Chain-walk through intermediate versions to find builds for all jobs.
	// Entry-point jobs consumed the tracked version directly, but downstream
	// jobs consume intermediate versions produced by upstream put steps.
	buildsByJob, err := q.chainWalkBuilds(ctx, tc, pn, versionID, chain)
	if err != nil {
		return nil, fmt.Errorf("failed to find builds by version chain walk: %w", err)
	}

	var path []resource.VersionPathEntry
	completed := 0
	for _, jn := range chain {
		entry := resource.VersionPathEntry{JobName: jn}
		if builds, ok := buildsByJob[jn]; ok && len(builds) > 0 {
			entry.Build = builds[0]
			if len(builds) > 1 {
				entry.Retries = builds[1:]
			}
			if redactLogs {
				redactBuildLogs(entry.Build)
				for _, rb := range entry.Retries {
					redactBuildLogs(rb)
				}
			}
			if isTerminalStatus(entry.Build.Status) {
				completed++
			}
		}
		path = append(path, entry)
	}

	return &resource.VersionPathResponse{
		Resource: resource.VersionPathResource{
			Canonical: rCan,
			Version:   version.Version,
		},
		Path:      path,
		Completed: completed,
		Total:     len(chain),
	}, nil
}

// redactBuildLogs clears all log content from a build's steps and job logs.
func redactBuildLogs(b *build.Build) {
	for i := range b.Steps {
		b.Steps[i].Logs = ""
	}
	for i := range b.Job {
		b.Job[i].Logs = ""
	}
}

// GetResourceVersionPath returns the version path for a specific resource version,
// showing which jobs the version passes through and the build status for each.
func (q *PikoCI) GetResourceVersionPath(ctx context.Context, tc, pn, rCan string, versionID uint32) (*resource.VersionPathResponse, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	}
	if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pn)
	}

	pp, err := q.GetPipeline(ctx, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to get Pipeline %q: %w", pn, err)
	}

	return q.resolveVersionPath(ctx, tc, pn, rCan, pp, versionID, false)
}

// GetPublicResourceVersionPath returns the version path for a public pipeline.
func (q *PikoCI) GetPublicResourceVersionPath(ctx context.Context, tc, pn, rCan string, versionID uint32) (*resource.VersionPathResponse, error) {
	pp, err := q.Pipelines.FindPublic(ctx, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	return q.resolveVersionPath(ctx, tc, pn, rCan, pp, versionID, true)
}

// chainWalkBuilds finds builds for all jobs in the chain by following version
// propagation through put/get steps. Entry-point jobs consumed the tracked
// version directly. For downstream jobs, we look at what versions the upstream
// builds produced (via build_get_versions) and find downstream builds that
// consumed those intermediate versions.
func (q *PikoCI) chainWalkBuilds(ctx context.Context, tc, pn string, versionID uint32, chain []string) (map[string][]*build.Build, error) {
	result := make(map[string][]*build.Build)
	coveredJobs := make(map[string]bool)

	// Start with the tracked version
	pendingVersionIDs := []uint32{versionID}
	seenVersionIDs := map[uint32]bool{versionID: true}

	// BFS: expand version IDs → builds → new version IDs
	for len(pendingVersionIDs) > 0 && len(coveredJobs) < len(chain) {
		// Find builds in uncovered chain jobs that consumed any of the pending versions
		var uncoveredJobs []string
		for _, jn := range chain {
			if !coveredJobs[jn] {
				uncoveredJobs = append(uncoveredJobs, jn)
			}
		}
		if len(uncoveredJobs) == 0 {
			break
		}

		// Query builds for each pending version ID
		var newBuildIDs []uint32
		for _, vid := range pendingVersionIDs {
			buildsByJob, err := q.Builds.FindByVersionAndJobs(ctx, tc, pn, vid, uncoveredJobs)
			if err != nil {
				return nil, fmt.Errorf("failed to find builds for version %d: %w", vid, err)
			}
			for jn, builds := range buildsByJob {
				if !coveredJobs[jn] && len(builds) > 0 {
					result[jn] = builds
					coveredJobs[jn] = true
					for _, b := range builds {
						newBuildIDs = append(newBuildIDs, b.ID)
					}
				}
			}
		}

		// From the newly found builds, collect all version IDs they
		// consumed/produced (their build_get_versions entries)
		pendingVersionIDs = nil
		for _, buildID := range newBuildIDs {
			getVersions, err := q.Builds.FindGetVersions(ctx, buildID)
			if err != nil {
				q.logger.Warn("chainWalkBuilds: failed to get versions for build", "build_id", buildID, "error", err)
				continue
			}
			for _, vid := range getVersions {
				if !seenVersionIDs[vid] {
					seenVersionIDs[vid] = true
					pendingVersionIDs = append(pendingVersionIDs, vid)
				}
			}
		}
	}

	return result, nil
}

// isTerminalStatus returns true if the build status is a terminal state.
func isTerminalStatus(s build.Status) bool {
	return s == build.Succeeded || s == build.Failed || s == build.Cancelled
}
