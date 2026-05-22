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
