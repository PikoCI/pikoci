resource "git" "repo" {
  params {
    url  = var.repo_url
    name = var.repo_name
  }
}

job "lint" {
  get "git" "repo" {
    trigger = true
  }
  task "lint" {
    run "docker" {
      image = "golang:1.25.1"
      cmd   = "cd ${var.repo_name} && go vet ./..."
    }
  }
}

job "test" {
  get "git" "repo" {
    trigger = true
  }
  task "test" {
    run "docker" {
      image = "golang:1.25.1"
      cmd   = "cd ${var.repo_name} && go test ./..."
    }
  }
}

job "build" {
  get "git" "repo" {
    trigger = true
    passed  = ["lint", "test"]
  }
  task "build" {
    run "docker" {
      image = "golang:1.25.1"
      cmd   = "cd ${var.repo_name} && go build ./..."
    }
  }
}

variable "repo_url" {
  type    = string
  default = "https://github.com/PikoCI/pikoci"
}

variable "repo_name" {
  type    = string
  default = "pikoci"
}
