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

job "by-passed" {
  get "artifact" "cron_output" {
    trigger = true
    passed  = ["gen"]
  }
  task "print-file" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- by-passed job ---' && cat cron-output/timestamp.txt"]
    }
  }
}

job "by-check" {
  get "artifact" "cron_output" {
    trigger = true
  }
  task "print-file" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo '--- by-check job ---' && cat cron-output/timestamp.txt"]
    }
  }
}
