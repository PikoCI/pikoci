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
  in_parallel {
    task "create-file" {
      run "exec" {
        path = "/bin/sh"
        args = ["-ec", "mkdir -p output && echo \"cron triggered at: $(date)\" > output/timestamp.txt && cat output/timestamp.txt"]
      }
    }
    task "create-summary" {
      run "exec" {
        path = "/bin/sh"
        args = ["-ec", "mkdir -p output && echo \"summary generated\" > output/summary.txt && cat output/summary.txt"]
      }
    }
  }
  put "artifact" "cron_output" {
    dir = "output"
  }
}

job "slow-build" {
  interruptible = true
  get "cron" "my_cron" {
    trigger = true
    passed  = ["gen"]
  }
  task "build" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo 'building...' && sleep 30 && echo 'done'"]
    }
  }
}

job "deploy-staging" {
  disable_retry = true

  input "version" {
    type        = "string"
    description = "Version to deploy"
    default     = "latest"
  }

  input "environment" {
    type    = "string"
    options = ["staging", "production"]
    default = "staging"
  }

  input "dry_run" {
    type    = "bool"
    default = "false"
  }

  get "artifact" "cron_output" {
    trigger = true
    passed  = ["gen"]
  }
  approve "deploy to staging" {}
  task "deploy" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- deploy-staging ---' && echo version=$input_version env=$input_environment dry_run=$input_dry_run && cat cron-output/timestamp.txt && echo 'deploying to staging...' && sleep 5 && echo 'done'"]
    }
  }
}

job "deploy-prod" {
  disable_retry = true
  get "artifact" "cron_output" {
    trigger = true
    passed  = ["gen"]
  }
  get "cron" "my_cron" {
    passed = ["gen"]
  }
  approve "deploy to production" {
    approvals = 2
  }
  task "deploy" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- deploy-prod ---' && cat cron-output/timestamp.txt && echo 'deploying to prod...' && sleep 5 && echo 'done'"]
    }
  }
}

job "validate" {
  for_each = toset(["lint", "vet"])
  get "cron" "my_cron" {
    trigger = true
    passed  = ["gen"]
  }
  task "run" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo 'validate ${each.value}'"]
    }
  }
}

job "deploy-after-lint" {
  get "cron" "my_cron" {
    trigger = true
    passed  = ["validate--lint"]
  }
  task "deploy" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo 'deploying after lint only'"]
    }
  }
}

job "validate-report" {
  get "cron" "my_cron" {
    trigger = true
    passed  = ["validate--lint", "validate--vet"]
  }
  task "report" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo 'all validations passed, generating report'"]
    }
  }
}

job "failing" {
  allow_failure = true
  get "cron" "my_cron" {
    trigger = true
    passed  = ["gen"]
  }
  task "fail" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo 'about to fail' && sleep 10 && exit 1"]
    }
  }
}

job "after-failing" {
  get "cron" "my_cron" {
    trigger = true
    passed  = ["failing"]
  }
  task "run" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo 'runs after failing (which has allow_failure=true)'"]
    }
  }
}

resource "cron" "nightly" {
  check_interval = "@every 60s"
}

job "nightly-cleanup" {
  get "cron" "nightly" {
    trigger = true
  }
  task "cleanup" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- nightly-cleanup ---' && echo 'cleaning old artifacts...' && sleep 2 && echo 'done'"]
    }
  }
}

job "nightly-report" {
  get "cron" "nightly" {
    trigger = true
    passed  = ["nightly-cleanup"]
  }
  task "report" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- nightly-report ---' && echo 'generating report...' && sleep 2 && echo 'done'"]
    }
  }
}

job "monitor" {
  input "target" {
    type        = "string"
    description = "Service to monitor"
    default     = "all"
  }

  input "verbose" {
    type    = "bool"
    default = "false"
  }

  get "cron" "my_cron" {
    trigger = true
    passed  = ["gen"]
  }
  task "check" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- monitor ---' && echo target=$input_target verbose=$input_verbose && echo 'cron version: $GET_MY_CRON_DATE' && echo 'done'"]
    }
    on_success "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo 'hook ok'"]
    }
    on_failure "exec" {
      path = "/bin/sh"
      args = ["-ec", "false"]
    }
  }
  on_success "exec" {
    path = "/bin/sh"
    args = ["-ec", "false"]
  }
  ensure "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo 'ensure ran'"]
  }
}
