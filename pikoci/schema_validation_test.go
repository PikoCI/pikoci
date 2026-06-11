package pikoci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePipelineSchema_ValidPipeline(t *testing.T) {
	hcl := []byte(`
resource_type "git" {
  check "exec" {
    args = ["/opt/resource/check"]
  }
  pull "exec" {
    args = ["/opt/resource/in"]
  }
  push "exec" {
    args = ["/opt/resource/out"]
  }
}

job "build" {
  get "git" "my-repo" {}
  task "compile" {
    run "exec" {
      args = ["make", "build"]
    }
  }
  put "git" "my-repo" {
    branch = "main"
  }
}
`)
	err := validatePipelineSchema(hcl)
	assert.NoError(t, err)
}

func TestValidatePipelineSchema_UnknownTopLevelBlock(t *testing.T) {
	hcl := []byte(`
resouce_type "git" {
  check "exec" {
    args = ["/opt/resource/check"]
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown block "resouce_type"`)
	assert.Contains(t, err.Error(), `did you mean "resource_type"`)
}

func TestValidatePipelineSchema_UnknownJobBlock(t *testing.T) {
	hcl := []byte(`
job "build" {
  tsk "compile" {
    run "exec" {
      args = ["make"]
    }
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown block "tsk"`)
	assert.Contains(t, err.Error(), `did you mean "task"`)
}

func TestValidatePipelineSchema_UnknownHookBlock(t *testing.T) {
	hcl := []byte(`
job "build" {
  task "compile" {
    run "exec" {
      args = ["make"]
    }
    on_succes "exec" {
      args = ["echo", "done"]
    }
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown block "on_succes"`)
	assert.Contains(t, err.Error(), `did you mean "on_success"`)
}

func TestValidatePipelineSchema_RunBlockArgTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  task "compile" {
    run "exec" {
      arg = ["make", "build"]
    }
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"arg"`)
	assert.Contains(t, err.Error(), `did you mean "args"`)
}

func TestValidatePipelineSchema_ResourceTypeArgTypo(t *testing.T) {
	hcl := []byte(`
resource_type "git" {
  check "exec" {
    arg = ["/opt/resource/check"]
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"arg"`)
	assert.Contains(t, err.Error(), `did you mean "args"`)
}

func TestValidatePipelineSchema_ReadyCheckTypo(t *testing.T) {
	hcl := []byte(`
service_type "postgres" {
  start "exec" {
    args = ["pg_ctl", "start"]
  }
  ready_check "exec" {
    args = ["pg_isready"]
    inteval = "5s"
  }
  stop "exec" {
    args = ["pg_ctl", "stop"]
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"inteval"`)
	assert.Contains(t, err.Error(), `did you mean "interval"`)
}

func TestValidatePipelineSchema_ReadyCheckTimeoutTypo(t *testing.T) {
	hcl := []byte(`
service_type "postgres" {
  start "exec" {
    args = ["pg_ctl", "start"]
  }
  ready_check "exec" {
    args = ["pg_isready"]
    timout = "30s"
  }
  stop "exec" {
    args = ["pg_ctl", "stop"]
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"timout"`)
	assert.Contains(t, err.Error(), `did you mean "timeout"`)
}

func TestValidatePipelineSchema_CustomParamsNotFlagged(t *testing.T) {
	hcl := []byte(`
resource_type "git" {
  check "exec" {
    args = ["/opt/resource/check"]
    url = "https://example.com"
    token = "abc"
  }
}
`)
	err := validatePipelineSchema(hcl)
	assert.NoError(t, err)
}

func TestValidatePipelineSchema_UnknownJobHookBlock(t *testing.T) {
	hcl := []byte(`
job "build" {
  on_succes "exec" {
    args = ["echo", "done"]
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown block "on_succes"`)
	assert.Contains(t, err.Error(), `did you mean "on_success"`)
}

func TestValidatePipelineSchema_PutStepHookTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  put "git" "my-repo" {
    branch = "main"
    on_faliure "exec" {
      args = ["echo", "failed"]
    }
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown block "on_faliure"`)
	assert.Contains(t, err.Error(), `did you mean "on_failure"`)
}

func TestValidatePipelineSchema_NotificationTypeArgTypo(t *testing.T) {
	hcl := []byte(`
notification_type "slack" {
  notify "exec" {
    arg = ["/opt/notify/send"]
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"arg"`)
	assert.Contains(t, err.Error(), `did you mean "args"`)
}

func TestValidatePipelineSchema_GetStepTriggerTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  get "git" "my-repo" {
    triggr = true
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"triggr"`)
	assert.Contains(t, err.Error(), `did you mean "trigger"`)
}

func TestValidatePipelineSchema_GetStepPassedTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  get "git" "my-repo" {
    passd = ["unit"]
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"passd"`)
	assert.Contains(t, err.Error(), `did you mean "passed"`)
}

func TestValidatePipelineSchema_GetStepTimeoutTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  get "git" "my-repo" {
    timout = "5m"
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"timout"`)
	assert.Contains(t, err.Error(), `did you mean "timeout"`)
}

func TestValidatePipelineSchema_GetStepAttemptsTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  get "git" "my-repo" {
    atempts = 3
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"atempts"`)
	assert.Contains(t, err.Error(), `did you mean "attempts"`)
}

func TestValidatePipelineSchema_TaskStepTimeoutTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  task "compile" {
    timout = "10m"
    run "exec" {
      args = ["make"]
    }
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"timout"`)
	assert.Contains(t, err.Error(), `did you mean "timeout"`)
}

func TestValidatePipelineSchema_TaskStepInputsTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  task "compile" {
    input = ["my-repo"]
    run "exec" {
      args = ["make"]
    }
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"input"`)
	assert.Contains(t, err.Error(), `did you mean "inputs"`)
}

func TestValidatePipelineSchema_TaskStepOutputsTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  task "compile" {
    output = ["artifact"]
    run "exec" {
      args = ["make"]
    }
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"output"`)
	assert.Contains(t, err.Error(), `did you mean "outputs"`)
}

func TestValidatePipelineSchema_PutStepTimeoutTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  put "git" "my-repo" {
    timout = "5m"
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"timout"`)
	assert.Contains(t, err.Error(), `did you mean "timeout"`)
}

func TestValidatePipelineSchema_PutStepAttemptsTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  put "git" "my-repo" {
    atempts = 3
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"atempts"`)
	assert.Contains(t, err.Error(), `did you mean "attempts"`)
}

func TestValidatePipelineSchema_NotifyStepMessageTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  notify "slack" "deploy" {
    mesage = "done"
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"mesage"`)
	assert.Contains(t, err.Error(), `did you mean "message"`)
}

func TestValidatePipelineSchema_InParallelBlock(t *testing.T) {
	hcl := []byte(`
job "test" {
  in_parallel {
    get "git" "repo" {}
    task "lint" {
      run "exec" { args = ["/bin/sh"] }
    }
  }
}
`)
	err := validatePipelineSchema(hcl)
	assert.NoError(t, err)
}

func TestValidatePipelineSchema_InParallelRejectsService(t *testing.T) {
	hcl := []byte(`
job "test" {
  in_parallel {
    service "db" {}
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service")
	assert.Contains(t, err.Error(), "not allowed")
}

func TestValidatePipelineSchema_InParallelRejectsNested(t *testing.T) {
	hcl := []byte(`
job "test" {
  in_parallel {
    in_parallel {}
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "in_parallel")
	assert.Contains(t, err.Error(), "not allowed")
}

func TestValidatePipelineSchema_JobConcurrencyTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  concurency = 2
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"concurency"`)
	assert.Contains(t, err.Error(), `did you mean "concurrency"`)
}

func TestValidatePipelineSchema_JobSerialGroupsTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  serial_group = ["deploy"]
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"serial_group"`)
	assert.Contains(t, err.Error(), `did you mean "serial_groups"`)
}

func TestValidatePipelineSchema_JobTimeoutTypo(t *testing.T) {
	hcl := []byte(`
job "build" {
  timout = "1h"
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"timout"`)
	assert.Contains(t, err.Error(), `did you mean "timeout"`)
}

func TestValidatePipelineSchema_ErrorIncludesLineNumber(t *testing.T) {
	hcl := []byte(`
job "build" {
  get "git" "my-repo" {
    triggr = true
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	// "triggr" is on line 4, col 5-11
	assert.Contains(t, err.Error(), "pipeline.hcl:4,")
}

func TestValidatePipelineSchema_BlockErrorIncludesLineNumber(t *testing.T) {
	hcl := []byte(`
job "build" {
  task "compile" {
    run "exec" {
      args = ["make"]
    }
    on_succes "exec" {
      args = ["echo", "done"]
    }
  }
}
`)
	err := validatePipelineSchema(hcl)
	require.Error(t, err)
	// "on_succes" block is on line 7
	assert.Contains(t, err.Error(), "pipeline.hcl:7,")
}

func TestValidatePipelineSchema_ValidGetStep(t *testing.T) {
	hcl := []byte(`
job "build" {
  get "git" "my-repo" {
    passed  = ["unit"]
    trigger = true
    timeout = "5m"
    attempts = 3
  }
}
`)
	err := validatePipelineSchema(hcl)
	assert.NoError(t, err)
}

func TestValidatePipelineSchema_ValidTaskStep(t *testing.T) {
	hcl := []byte(`
job "build" {
  task "compile" {
    timeout  = "10m"
    attempts = 2
    inputs   = ["my-repo"]
    outputs  = ["artifact"]
    run "exec" {
      args = ["make"]
    }
  }
}
`)
	err := validatePipelineSchema(hcl)
	assert.NoError(t, err)
}

func TestValidatePipelineSchema_ValidJob(t *testing.T) {
	hcl := []byte(`
job "build" {
  concurrency   = 2
  serial_groups = ["deploy"]
  timeout       = "1h"
}
`)
	err := validatePipelineSchema(hcl)
	assert.NoError(t, err)
}
