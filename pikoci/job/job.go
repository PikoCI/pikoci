// Package job defines the domain model for pipeline jobs in PikoCI.
// A job consists of a plan with ordered steps (get, put, task, service, runner)
// and optional lifecycle hooks for success, failure, cancellation, and cleanup.
package job

import (
	"time"

	"github.com/pikoci/pikoci/pikoci/utils"
)

// StepType identifies the kind of step in a job plan.
type StepType string

const (
	// StepTypeGet represents a step that fetches a resource version.
	StepTypeGet StepType = "get"
	// StepTypeTask represents a step that runs an arbitrary command.
	StepTypeTask StepType = "task"
	// StepTypePut represents a step that pushes a new resource version.
	StepTypePut StepType = "put"
	// StepTypeService represents a step that starts a sidecar service.
	StepTypeService StepType = "service"
	// StepTypeRunner represents a step that executes a runner command.
	StepTypeRunner StepType = "runner"
	// StepTypeNotify represents a step that sends a notification.
	StepTypeNotify StepType = "notify"
)

// HookStep represents a single step inside a hook (on_success, on_failure, on_cancel, ensure).
// It can be a runner command, a put step, or a notify step.
type HookStep struct {
	Type   StepType             `json:"type"`
	Runner *utils.RunnerCommand `json:"runner,omitempty"`
	Put    *PutStep             `json:"put,omitempty"`
	Notify *NotifyStep          `json:"notify,omitempty"`
}

// Job represents a named unit of work in a pipeline. It contains a plan of
// ordered steps and optional lifecycle hooks that run on success, failure,
// cancellation, or unconditionally (ensure).
type Job struct {
	ID          uint32     `json:"id"`
	Name        string     `json:"name" hcl:"name,label"`
	Concurrency int        `json:"concurrency,omitempty"`
	Paused      bool          `json:"paused"`
	Timeout     time.Duration `json:"timeout,omitempty"`
	Plan        []PlanStep    `json:"plan"`

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

// PlanStep represents a single step in a job's plan. Exactly one of Get, Task,
// Put, or Service is set, determined by the Type field. Each step may also
// define its own lifecycle hooks and retry/timeout configuration.
type PlanStep struct {
	Type      StepType      `json:"type"`
	Timeout   time.Duration `json:"timeout,omitempty"`
	Attempts  int           `json:"attempts,omitempty"`
	Get       *GetStep      `json:"get,omitempty"`
	Task      *TaskStep     `json:"task,omitempty"`
	Put       *PutStep      `json:"put,omitempty"`
	Notify    *NotifyStep   `json:"notify,omitempty"`
	Service   *ServiceStep  `json:"service,omitempty"`
	OnSuccess []HookStep    `json:"on_success,omitempty"`
	OnFailure []HookStep    `json:"on_failure,omitempty"`
	OnCancel  []HookStep    `json:"on_cancel,omitempty"`
	Ensure    []HookStep    `json:"ensure,omitempty"`
}

// GetStep defines a step that fetches a specific resource version. The Type and
// Name fields identify the resource, Passed constrains which upstream jobs must
// have succeeded, and Trigger controls whether new versions automatically start builds.
type GetStep struct {
	Type    string   `json:"type" hcl:"type,label"`
	Name    string   `json:"name" hcl:"name,label"`
	Passed  []string `json:"passed" hcl:"passed,optional"`
	Trigger bool     `json:"trigger" hcl:"trigger,optional"`
}

// ResourceCanonical returns the canonical identifier for the resource
// referenced by this get step, combining the type and name.
func (g *GetStep) ResourceCanonical() string {
	return utils.ResourceCanonical(g.Type, g.Name)
}

// TaskStep defines a step that runs an arbitrary command using a runner.
// Inputs and Outputs specify which resources are mounted into and collected
// from the task's working directory.
type TaskStep struct {
	Name    string              `json:"name" hcl:"name,label"`
	Run     utils.RunnerCommand `json:"run" hcl:"run,block"`
	Inputs  []string            `json:"inputs,omitempty"`
	Outputs []string            `json:"outputs,omitempty"`
}

// PutStep defines a step that pushes a new version of a resource.
// Params are passed to the resource type's push command.
type PutStep struct {
	Type   string            `json:"type"`
	Name   string            `json:"name"`
	Params map[string]string `json:"params,omitempty"`
}

// ResourceCanonical returns the canonical identifier for the resource
// referenced by this put step, combining the type and name.
func (p *PutStep) ResourceCanonical() string {
	return utils.ResourceCanonical(p.Type, p.Name)
}

// NotifyStep defines a step that sends a notification.
// Params are passed to the notification type's notify command.
type NotifyStep struct {
	Type    string            `json:"type"`
	Name    string            `json:"name"`
	Params  map[string]string `json:"params,omitempty"`
	Message string            `json:"message,omitempty"`
}

// NotificationCanonical returns the canonical identifier for the notification
// referenced by this notify step, combining the type and name.
func (n *NotifyStep) NotificationCanonical() string {
	return utils.NotificationCanonical(n.Type, n.Name)
}

// ServiceStep defines a step that starts a sidecar service during job execution.
// Params are passed to the service type's start command.
type ServiceStep struct {
	Name   string            `json:"name"`
	Params map[string]string `json:"params,omitempty"`
}
