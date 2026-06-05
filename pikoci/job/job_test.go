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

func TestNotifyStep_NotificationCanonical(t *testing.T) {
	n := &job.NotifyStep{Type: "slack", Name: "deploy-alerts"}
	assert.Equal(t, "slack.deploy-alerts", n.NotificationCanonical())

	n = &job.NotifyStep{Type: "email", Name: "ops"}
	assert.Equal(t, "email.ops", n.NotificationCanonical())
}
