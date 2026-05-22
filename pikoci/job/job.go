package job

import (
	"time"

	"github.com/xescugc/pikoci/pikoci/utils"
)

type StepType string

const (
	StepTypeGet     StepType = "get"
	StepTypeTask    StepType = "task"
	StepTypePut     StepType = "put"
	StepTypeService StepType = "service"
	StepTypeRunner  StepType = "runner"
)

// HookStep represents a single step inside a hook (on_success, on_failure, on_cancel, ensure).
// It can be either a runner command or a put step.
type HookStep struct {
	Type   StepType             `json:"type"`
	Runner *utils.RunnerCommand `json:"runner,omitempty"`
	Put    *PutStep             `json:"put,omitempty"`
}

type Job struct {
	ID          uint32     `json:"id"`
	Name        string     `json:"name" hcl:"name,label"`
	Concurrency int        `json:"concurrency,omitempty"`
	Plan        []PlanStep `json:"plan"`

	OnSuccess []HookStep `json:"on_success,omitempty"`
	OnFailure []HookStep `json:"on_failure,omitempty"`
	OnCancel  []HookStep `json:"on_cancel,omitempty"`
	Ensure    []HookStep `json:"ensure,omitempty"`
}

// GetSteps returns all get steps from the plan in order.
func (j *Job) GetSteps() []GetStep {
	var steps []GetStep
	for _, p := range j.Plan {
		if p.Type == StepTypeGet && p.Get != nil {
			steps = append(steps, *p.Get)
		}
	}
	return steps
}

// AllPutSteps returns all put steps from the plan and from hooks (on_success,
// on_failure, on_cancel, ensure) at both step and job level.
func (j *Job) AllPutSteps() []PutStep {
	seen := make(map[string]bool)
	var steps []PutStep
	add := func(p *PutStep) {
		if p == nil {
			return
		}
		key := p.Type + "." + p.Name
		if !seen[key] {
			seen[key] = true
			steps = append(steps, *p)
		}
	}
	collectHooks := func(hooks []HookStep) {
		for _, h := range hooks {
			if h.Type == StepTypePut {
				add(h.Put)
			}
		}
	}
	for _, p := range j.Plan {
		if p.Type == StepTypePut {
			add(p.Put)
		}
		collectHooks(p.OnSuccess)
		collectHooks(p.OnFailure)
		collectHooks(p.OnCancel)
		collectHooks(p.Ensure)
	}
	collectHooks(j.OnSuccess)
	collectHooks(j.OnFailure)
	collectHooks(j.OnCancel)
	collectHooks(j.Ensure)
	return steps
}

// PlanGetSteps returns all PlanSteps that are get steps.
func (j *Job) PlanGetSteps() []PlanStep {
	var steps []PlanStep
	for _, p := range j.Plan {
		if p.Type == StepTypeGet {
			steps = append(steps, p)
		}
	}
	return steps
}

type PlanStep struct {
	Type      StepType              `json:"type"`
	Timeout   time.Duration         `json:"timeout,omitempty"`
	Attempts  int                   `json:"attempts,omitempty"`
	Get       *GetStep              `json:"get,omitempty"`
	Task      *TaskStep             `json:"task,omitempty"`
	Put       *PutStep              `json:"put,omitempty"`
	Service   *ServiceStep          `json:"service,omitempty"`
	OnSuccess []HookStep `json:"on_success,omitempty"`
	OnFailure []HookStep `json:"on_failure,omitempty"`
	OnCancel  []HookStep `json:"on_cancel,omitempty"`
	Ensure    []HookStep `json:"ensure,omitempty"`
}

type GetStep struct {
	Type    string   `json:"type" hcl:"type,label"`
	Name    string   `json:"name" hcl:"name,label"`
	Passed  []string `json:"passed" hcl:"passed,optional"`
	Trigger bool     `json:"trigger" hcl:"trigger,optional"`
}

func (g *GetStep) ResourceCanonical() string {
	return utils.ResourceCanonical(g.Type, g.Name)
}

type TaskStep struct {
	Name    string              `json:"name" hcl:"name,label"`
	Run     utils.RunnerCommand `json:"run" hcl:"run,block"`
	Inputs  []string            `json:"inputs,omitempty"`
	Outputs []string            `json:"outputs,omitempty"`
}

type PutStep struct {
	Type   string            `json:"type"`
	Name   string            `json:"name"`
	Params map[string]string `json:"params,omitempty"`
}

func (p *PutStep) ResourceCanonical() string {
	return utils.ResourceCanonical(p.Type, p.Name)
}

type ServiceStep struct {
	Name   string            `json:"name"`
	Params map[string]string `json:"params,omitempty"`
}
