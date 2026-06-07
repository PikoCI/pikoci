secret_type "my-file" {
  source = "pikoci://file"
  path   = "pikoci/testdata/secrets.json"
}

variable "greeting" {
  type = string
  secret "my-file" {
    key = "greeting"
  }
}

variable "env" {
  type = string
  secret "my-file" {
    key = "env"
  }
}

resource "cron" "my_cron" {
  check_interval = "@every 20s"
}

resource "artifact" "cron_output" {
  params {
    dir = "cron-output"
  }
}

job "gen" {
  get "cron" "my_cron" {
    trigger = true
  }
  task "create-file" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "mkdir -p output && echo \"cron triggered at: $(date)\" > output/timestamp.txt && cat output/timestamp.txt"]
    }
  }
  put "artifact" "cron_output" {
    dir = "output"
  }
}

job "deploy-staging" {
  serial_groups = ["deploy"]

  get "artifact" "cron_output" {
    trigger = true
    passed  = ["gen"]
  }
  task "deploy" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- deploy-staging ---' && cat cron-output/timestamp.txt && echo 'deploying to staging...' && sleep 10 && echo 'done'"]
    }
  }
}

job "deploy-prod" {
  serial_groups = ["deploy"]

  get "artifact" "cron_output" {
    trigger = true
    passed  = ["gen"]
  }
  task "deploy" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- deploy-prod ---' && cat cron-output/timestamp.txt && echo 'deploying to prod...' && sleep 10 && echo 'done'"]
    }
  }
}

job "validate" {
  for_each = toset(["format", "lint", "vet"])

  get "artifact" "cron_output" {
    trigger = true
    passed  = ["gen"]
  }
  task "run-check" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo 'running ${each.value} check...' && cat cron-output/timestamp.txt && echo '${each.value}: ok'"]
    }
  }
}

job "notify" {
  for_each = {
    "slack"   = "#deploys"
    "discord" = "#ci-alerts"
  }

  get "artifact" "cron_output" {
    trigger = true
    passed  = ["validate"]
  }
  task "send" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo 'notifying ${each.key} channel ${each.value}...'"]
    }
  }
}

job "deploy-matrix" {
  matrix {
    env    = ["staging", "prod"]
    region = ["us", "eu"]
  }

  get "artifact" "cron_output" {
    trigger = true
    passed  = ["notify"]
  }
  task "deploy" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo 'deploying to ${each.value.env}/${each.value.region}...' && cat cron-output/timestamp.txt && echo 'done'"]
    }
  }
}
