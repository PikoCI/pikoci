package pikoci

import (
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
)

// ReachableJobs returns all jobs reachable when the given resource canonical
// triggers a pipeline, i.e. the jobs whose on_trigger hooks must fire. It
// starts from direct-trigger jobs (get step matching rCan with no passed
// constraints) and follows passed dependencies via BFS.
func ReachableJobs(p *pipeline.Pipeline, rCan string) []job.Job {
	// Step 1: find direct-trigger jobs.
	directNames := make(map[string]bool)
	for _, j := range p.Jobs {
		for _, ps := range j.FlatPlanSteps() {
			if ps.Type != job.StepTypeGet || ps.Get == nil {
				continue
			}
			g := ps.Get
			if g.ResourceCanonical() == rCan && len(g.Passed) == 0 && g.Trigger {
				directNames[j.Name] = true
				break
			}
		}
	}

	// Step 2: BFS over passed constraints to find transitively reachable jobs.
	reachableNames := make(map[string]bool, len(directNames))
	for n := range directNames {
		reachableNames[n] = true
	}

	queue := make([]string, 0, len(directNames))
	for n := range directNames {
		queue = append(queue, n)
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, j := range p.Jobs {
			if reachableNames[j.Name] {
				continue
			}
			// Check if j depends on 'current' in any get step's passed list.
			for _, ps := range j.FlatPlanSteps() {
				if ps.Type != job.StepTypeGet || ps.Get == nil {
					continue
				}
				g := ps.Get
				if len(g.Passed) == 0 {
					continue
				}
				expanded := ResolvePassedJobNames(g.Passed, p.Jobs)
				for _, passed := range expanded {
					if passed == current {
						reachableNames[j.Name] = true
						queue = append(queue, j.Name)
						break
					}
				}
				if reachableNames[j.Name] {
					break
				}
			}
		}
	}

	// Return jobs in pipeline declaration order.
	var result []job.Job
	for _, j := range p.Jobs {
		if reachableNames[j.Name] {
			result = append(result, j)
		}
	}
	return result
}
