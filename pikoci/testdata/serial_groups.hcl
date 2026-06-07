resource "git" "repo" {
  params {
    url = "https://example.com/repo.git"
  }
}

job "deploy-staging" {
  serial_groups = ["deploy"]

  get "git" "repo" {
    trigger = true
  }
  task "deploy" {
    run "exec" {
      path = "./deploy.sh"
    }
  }
}

job "deploy-prod" {
  serial_groups = ["deploy"]

  get "git" "repo" {
    passed = ["deploy-staging"]
  }
  task "deploy" {
    run "exec" {
      path = "./deploy.sh"
    }
  }
}
