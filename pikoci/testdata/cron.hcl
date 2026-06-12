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
  get "artifact" "cron_output" {
    trigger = true
    passed  = ["gen"]
  }
  task "deploy" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- deploy-staging ---' && cat cron-output/timestamp.txt && echo 'deploying to staging...' && sleep 5 && echo 'done'"]
    }
  }
}

job "deploy-prod" {
  get "artifact" "cron_output" {
    trigger = true
    passed  = ["gen"]
  }
  task "deploy" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- deploy-prod ---' && cat cron-output/timestamp.txt && echo 'deploying to prod...' && sleep 5 && echo 'done'"]
    }
  }
}

job "monitor" {
  get "cron" "my_cron" {
    trigger = true
    passed  = ["gen"]
  }
  task "check" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- monitor ---' && echo 'cron version: $GET_MY_CRON_DATE' && echo 'done'"]
    }
  }
}
