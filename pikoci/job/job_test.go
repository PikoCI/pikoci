package job_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/utils"
)

func TestGetStep_ResourceCanonical(t *testing.T) {
	g := &job.GetStep{Type: "git", Name: "my-repo"}
	assert.Equal(t, "git.my-repo", g.ResourceCanonical())

	g = &job.GetStep{Type: "cron", Name: "timer"}
	assert.Equal(t, "cron.timer", g.ResourceCanonical())
}

func TestPutStep_ResourceCanonical(t *testing.T) {
	p := &job.PutStep{Type: "git", Name: "my-repo"}
	assert.Equal(t, "git.my-repo", p.ResourceCanonical())

	p = &job.PutStep{Type: "docker", Name: "image"}
	assert.Equal(t, "docker.image", p.ResourceCanonical())
}

func TestJob_GetSteps(t *testing.T) {
	j := job.Job{
		Name: "test",
		Plan: []job.PlanStep{
			{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo"}},
			{Type: job.StepTypeTask, Task: &job.TaskStep{Name: "build"}},
			{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
			{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "repo"}},
		},
	}

	gets := j.GetSteps()
	require.Len(t, gets, 2)
	assert.Equal(t, "repo", gets[0].Name)
	assert.Equal(t, "timer", gets[1].Name)
}

func TestJob_GetSteps_Empty(t *testing.T) {
	j := job.Job{Name: "empty"}
	gets := j.GetSteps()
	assert.Nil(t, gets)
}

func TestJob_PlanGetSteps(t *testing.T) {
	j := job.Job{
		Name: "test",
		Plan: []job.PlanStep{
			{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo"}},
			{Type: job.StepTypeTask, Task: &job.TaskStep{Name: "build"}},
			{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
		},
	}

	planGets := j.PlanGetSteps()
	require.Len(t, planGets, 2)
	assert.Equal(t, job.StepTypeGet, planGets[0].Type)
	assert.Equal(t, job.StepTypeGet, planGets[1].Type)
}

func TestPlanStep_JSONMarshalUnmarshal(t *testing.T) {
	original := []job.PlanStep{
		{
			Type: job.StepTypeGet,
			Get:  &job.GetStep{Type: "git", Name: "repo", Trigger: true},
		},
		{
			Type: job.StepTypeTask,
			Task: &job.TaskStep{
				Name: "build",
				Run:  utils.RunnerCommand{Runner: "exec", Params: map[string]string{"path": "echo"}},
			},
		},
		{
			Type: job.StepTypePut,
			Put:  &job.PutStep{Type: "docker", Name: "image", Params: map[string]string{"tag": "latest"}},
			OnSuccess: []job.HookStep{
				{Type: job.StepTypeRunner, Runner: &utils.RunnerCommand{Runner: "exec", Args: []string{"done"}, Params: map[string]string{"path": "echo"}}},
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded []job.PlanStep
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	require.Len(t, decoded, 3)
	assert.Equal(t, job.StepTypeGet, decoded[0].Type)
	assert.NotNil(t, decoded[0].Get)
	assert.Nil(t, decoded[0].Task)
	assert.Nil(t, decoded[0].Put)
	assert.Equal(t, "repo", decoded[0].Get.Name)
	assert.True(t, decoded[0].Get.Trigger)

	assert.Equal(t, job.StepTypeTask, decoded[1].Type)
	assert.NotNil(t, decoded[1].Task)
	assert.Equal(t, "build", decoded[1].Task.Name)

	assert.Equal(t, job.StepTypePut, decoded[2].Type)
	assert.NotNil(t, decoded[2].Put)
	assert.Equal(t, "latest", decoded[2].Put.Params["tag"])
	require.Len(t, decoded[2].OnSuccess, 1)
}

func TestJob_AllPutSteps(t *testing.T) {
	t.Run("collects put steps from plan", func(t *testing.T) {
		j := job.Job{
			Name: "test",
			Plan: []job.PlanStep{
				{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo"}},
				{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "repo"}},
				{Type: job.StepTypePut, Put: &job.PutStep{Type: "docker", Name: "image"}},
			},
		}

		puts := j.AllPutSteps()
		require.Len(t, puts, 2)
		assert.Equal(t, "repo", puts[0].Name)
		assert.Equal(t, "git", puts[0].Type)
		assert.Equal(t, "image", puts[1].Name)
		assert.Equal(t, "docker", puts[1].Type)
	})

	t.Run("deduplicates put steps", func(t *testing.T) {
		j := job.Job{
			Name: "test",
			Plan: []job.PlanStep{
				{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "repo"}},
				{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "repo"}},
			},
		}

		puts := j.AllPutSteps()
		require.Len(t, puts, 1)
		assert.Equal(t, "repo", puts[0].Name)
	})

	t.Run("collects put steps from step-level hooks", func(t *testing.T) {
		j := job.Job{
			Name: "test",
			Plan: []job.PlanStep{
				{
					Type: job.StepTypeTask,
					Task: &job.TaskStep{Name: "build"},
					OnSuccess: []job.HookStep{
						{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "repo"}},
					},
					OnFailure: []job.HookStep{
						{Type: job.StepTypePut, Put: &job.PutStep{Type: "slack", Name: "alerts"}},
					},
					OnCancel: []job.HookStep{
						{Type: job.StepTypePut, Put: &job.PutStep{Type: "docker", Name: "cleanup"}},
					},
					Ensure: []job.HookStep{
						{Type: job.StepTypePut, Put: &job.PutStep{Type: "s3", Name: "logs"}},
					},
				},
			},
		}

		puts := j.AllPutSteps()
		require.Len(t, puts, 4)
	})

	t.Run("collects put steps from job-level hooks", func(t *testing.T) {
		j := job.Job{
			Name: "test",
			Plan: []job.PlanStep{
				{Type: job.StepTypeTask, Task: &job.TaskStep{Name: "build"}},
			},
			OnSuccess: []job.HookStep{
				{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "success-put"}},
			},
			OnFailure: []job.HookStep{
				{Type: job.StepTypePut, Put: &job.PutStep{Type: "slack", Name: "fail-put"}},
			},
			OnCancel: []job.HookStep{
				{Type: job.StepTypePut, Put: &job.PutStep{Type: "docker", Name: "cancel-put"}},
			},
			Ensure: []job.HookStep{
				{Type: job.StepTypePut, Put: &job.PutStep{Type: "s3", Name: "ensure-put"}},
			},
		}

		puts := j.AllPutSteps()
		require.Len(t, puts, 4)
	})

	t.Run("skips nil put in hook steps", func(t *testing.T) {
		j := job.Job{
			Name: "test",
			Plan: []job.PlanStep{
				{
					Type: job.StepTypeTask,
					Task: &job.TaskStep{Name: "build"},
					OnSuccess: []job.HookStep{
						{Type: job.StepTypeRunner, Runner: &utils.RunnerCommand{Runner: "exec"}},
					},
				},
			},
		}

		puts := j.AllPutSteps()
		assert.Nil(t, puts)
	})

	t.Run("returns nil for empty job", func(t *testing.T) {
		j := job.Job{Name: "empty"}
		puts := j.AllPutSteps()
		assert.Nil(t, puts)
	})

	t.Run("combines plan and hook put steps with dedup", func(t *testing.T) {
		j := job.Job{
			Name: "test",
			Plan: []job.PlanStep{
				{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "repo"}},
			},
			OnSuccess: []job.HookStep{
				{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "repo"}},
				{Type: job.StepTypePut, Put: &job.PutStep{Type: "slack", Name: "notify"}},
			},
		}

		puts := j.AllPutSteps()
		require.Len(t, puts, 2)
		assert.Equal(t, "repo", puts[0].Name)
		assert.Equal(t, "notify", puts[1].Name)
	})
}

func TestGetSteps_IncludesInParallelSteps(t *testing.T) {
	j := job.Job{
		Plan: []job.PlanStep{
			{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo-a"}},
			{Type: job.StepTypeInParallel, InParallel: &job.InParallelStep{
				Steps: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo-b"}},
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo-c"}},
				},
			}},
		},
	}
	steps := j.GetSteps()
	require.Len(t, steps, 3)
	assert.Equal(t, "repo-a", steps[0].Name)
	assert.Equal(t, "repo-b", steps[1].Name)
	assert.Equal(t, "repo-c", steps[2].Name)
}

func TestAllPutSteps_IncludesInParallelSteps(t *testing.T) {
	j := job.Job{
		Plan: []job.PlanStep{
			{Type: job.StepTypeInParallel, InParallel: &job.InParallelStep{
				Steps: []job.PlanStep{
					{Type: job.StepTypePut, Put: &job.PutStep{Type: "s3", Name: "upload"}},
				},
			}},
		},
	}
	steps := j.AllPutSteps()
	require.Len(t, steps, 1)
	assert.Equal(t, "upload", steps[0].Name)
}

func TestFlatPlanSteps(t *testing.T) {
	j := job.Job{
		Plan: []job.PlanStep{
			{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo-a"}},
			{Type: job.StepTypeInParallel, InParallel: &job.InParallelStep{
				Steps: []job.PlanStep{
					{Type: job.StepTypeTask, Task: &job.TaskStep{Name: "lint"}},
					{Type: job.StepTypeTask, Task: &job.TaskStep{Name: "test"}},
				},
			}},
			{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "repo-a"}},
		},
	}
	flat := j.FlatPlanSteps()
	require.Len(t, flat, 4)
	assert.Equal(t, job.StepTypeGet, flat[0].Type)
	assert.Equal(t, job.StepTypeTask, flat[1].Type)
	assert.Equal(t, "lint", flat[1].Task.Name)
	assert.Equal(t, job.StepTypeTask, flat[2].Type)
	assert.Equal(t, "test", flat[2].Task.Name)
	assert.Equal(t, job.StepTypePut, flat[3].Type)
}

func TestPlanGetSteps_IncludesInParallelSteps(t *testing.T) {
	j := job.Job{
		Plan: []job.PlanStep{
			{Type: job.StepTypeInParallel, InParallel: &job.InParallelStep{
				Steps: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "inner"}},
				},
			}},
		},
	}
	steps := j.PlanGetSteps()
	require.Len(t, steps, 1)
	assert.Equal(t, job.StepTypeGet, steps[0].Type)
}

func TestFlatPlanSteps_IfBranches(t *testing.T) {
	j := job.Job{
		Name: "test",
		Plan: []job.PlanStep{
			{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "app"}},
			{
				Type: job.StepTypeIf,
				If: &job.IfStep{
					Branches: []job.IfBranch{
						{
							Type:      "if",
							Label:     "check-prod",
							Condition: "$BRANCH == 'main'",
							Steps: []job.PlanStep{
								{Type: job.StepTypeTask, Task: &job.TaskStep{Name: "deploy-prod"}},
								{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "release"}},
							},
						},
						{
							Type: "else",
							Steps: []job.PlanStep{
								{Type: job.StepTypeTask, Task: &job.TaskStep{Name: "skip"}},
							},
						},
					},
				},
			},
			{Type: job.StepTypeTask, Task: &job.TaskStep{Name: "notify"}},
		},
	}

	flat := j.FlatPlanSteps()
	require.Len(t, flat, 5)
	assert.Equal(t, job.StepTypeGet, flat[0].Type)
	assert.Equal(t, "deploy-prod", flat[1].Task.Name)
	assert.Equal(t, "release", flat[2].Put.Name)
	assert.Equal(t, "skip", flat[3].Task.Name)
	assert.Equal(t, "notify", flat[4].Task.Name)
}

func TestFlatPlanSteps_IfOnly(t *testing.T) {
	j := job.Job{
		Name: "test",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeIf,
				If: &job.IfStep{
					Branches: []job.IfBranch{
						{
							Type:      "if",
							Condition: "$BRANCH == 'main'",
							Steps: []job.PlanStep{
								{Type: job.StepTypeTask, Task: &job.TaskStep{Name: "only-task"}},
							},
						},
					},
				},
			},
		},
	}

	flat := j.FlatPlanSteps()
	require.Len(t, flat, 1)
	assert.Equal(t, "only-task", flat[0].Task.Name)
}

func TestGetSteps_InsideIfBranches(t *testing.T) {
	j := job.Job{
		Name: "test",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeIf,
				If: &job.IfStep{
					Branches: []job.IfBranch{
						{
							Type: "if",
							Steps: []job.PlanStep{
								{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "app", Trigger: true}},
							},
						},
						{
							Type: "else",
							Steps: []job.PlanStep{
								{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "backup"}},
							},
						},
					},
				},
			},
		},
	}

	gets := j.GetSteps()
	require.Len(t, gets, 2)
	assert.Equal(t, "app", gets[0].Name)
	assert.Equal(t, "backup", gets[1].Name)
}

func TestAllPutSteps_InsideIfBranches(t *testing.T) {
	j := job.Job{
		Name: "test",
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeIf,
				If: &job.IfStep{
					Branches: []job.IfBranch{
						{
							Type: "if",
							Steps: []job.PlanStep{
								{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "release"}},
							},
						},
					},
				},
			},
		},
	}

	puts := j.AllPutSteps()
	require.Len(t, puts, 1)
	assert.Equal(t, "release", puts[0].Name)
}

func TestIfStep_JSONRoundTrip(t *testing.T) {
	original := job.PlanStep{
		Type: job.StepTypeIf,
		If: &job.IfStep{
			Branches: []job.IfBranch{
				{
					Type:      "if",
					Label:     "check-branch",
					Condition: "$GET_APP_BRANCH == 'main'",
					Steps: []job.PlanStep{
						{Type: job.StepTypeTask, Task: &job.TaskStep{Name: "deploy"}},
					},
				},
				{
					Type:      "else_if",
					Label:     "check-staging",
					Condition: "$GET_APP_BRANCH == 'staging'",
					Steps:     []job.PlanStep{},
				},
				{
					Type:  "else",
					Steps: []job.PlanStep{},
				},
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded job.PlanStep
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	require.NotNil(t, decoded.If)
	require.Len(t, decoded.If.Branches, 3)
	assert.Equal(t, "if", decoded.If.Branches[0].Type)
	assert.Equal(t, "check-branch", decoded.If.Branches[0].Label)
	assert.Equal(t, "$GET_APP_BRANCH == 'main'", decoded.If.Branches[0].Condition)
	assert.Equal(t, "else_if", decoded.If.Branches[1].Type)
	assert.Equal(t, "else", decoded.If.Branches[2].Type)
	assert.Empty(t, decoded.If.Branches[2].Condition)
}

func TestNotifyStep_NotificationCanonical(t *testing.T) {
	n := &job.NotifyStep{Type: "slack", Name: "deploy-alerts"}
	assert.Equal(t, "slack.deploy-alerts", n.NotificationCanonical())

	n = &job.NotifyStep{Type: "email", Name: "ops"}
	assert.Equal(t, "email.ops", n.NotificationCanonical())
}
