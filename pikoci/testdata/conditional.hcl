resource "cron" "trigger" {
  check_interval = "@every 30s"
}

job "conditional-demo" {
  get "cron" "trigger" {
    trigger = true
  }

  task "detect-env" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "printf 'ENV=staging\\n' > $PIKOCI_OUTPUT"]
    }
  }

  if "check-prod" {
    condition = "$TASK_DETECT_ENV_ENV == 'production'"
    task "deploy-prod" {
      run "exec" {
        path = "echo"
        args = ["Deploying to production"]
      }
    }
  }

  else_if "check-staging" {
    condition = "$TASK_DETECT_ENV_ENV == 'staging'"
    task "deploy-staging" {
      run "exec" {
        path = "echo"
        args = ["Deploying to staging"]
      }
    }
  }

  else {
    task "skip-deploy" {
      run "exec" {
        path = "echo"
        args = ["Unknown environment, skipping deploy"]
      }
    }
  }
}
