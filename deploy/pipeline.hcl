# --- Jobs: CI ---

job "lint" {
  get "git" "pikoci_pr" {
    trigger = true
  }
  put "github-check" "ci" { status = "in_progress" }
  task "make" {
    run "docker" {
      image = "golang:1.25.1"
      cmd   = "cd ${var.git_name} && make lint"
      args  = [
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
      ]
    }
  }
  on_success {
    put "github-check" "ci" { conclusion = "success" }
  }
  on_failure {
    put "github-check" "ci" { conclusion = "failure" }
  }
}

job "test-mock" {
  get "git" "pikoci_pr" {
    trigger = true
  }
  put "github-check" "ci" { status = "in_progress" }
  task "make" {
    run "docker" {
      image = "golang:1.25.1"
      cmd   = "cd ${var.git_name} && make test-mock"
      args  = [
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
      ]
    }
  }
  on_success {
    put "github-check" "ci" { conclusion = "success" }
  }
  on_failure {
    put "github-check" "ci" { conclusion = "failure" }
  }
}

job "test-integration" {
  get "git" "pikoci_pr" {
    trigger = true
  }
  put "github-check" "ci" { status = "in_progress" }
  task "make" {
    run "docker" {
      image = "ghcr.io/xescugc/pikoci-integration:latest"
      cmd   = <<-EOT
        cd ${var.git_name}
        cp /usr/local/bin/geckodriver integration/vendor/geckodriver
        make test-integration
      EOT
      args  = [
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
      ]
    }
  }
  on_success {
    put "github-check" "ci" { conclusion = "success" }
  }
  on_failure {
    put "github-check" "ci" { conclusion = "failure" }
  }
}

job "test-backends" {
  get "git" "pikoci_pr" {
    trigger = true
    passed  = ["lint", "test-mock", "test-integration"]
  }

  put "github-check" "ci" { status = "in_progress" }

  service "mariadb" {
    version       = "11.4.2"
    port          = "3306"
    root_password = "root123"
  }
  service "postgresql" {
    version  = "17"
    port     = "5432"
    password = "postgres123"
  }
  service "nats" {
    version = "2.12.0"
    port    = "4222"
  }
  service "rabbitmq" {
    version = "3"
    port    = "5672"
  }
  service "kafka" {
    version = "latest"
    port    = "9092"
  }
  service "vault" {
    version    = "latest"
    port       = "8200"
    root_token = "test-root-token"
  }

  task "make" {
    run "docker" {
      image = "golang:1.25.1"
      cmd   = "cd ${var.git_name} && make test-backends"
      args  = [
        "--network=host",
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
      ]
    }
  }

  on_success {
    put "github-check" "ci" { conclusion = "success" }
  }
  on_failure {
    put "github-check" "ci" { conclusion = "failure" }
  }
}

# --- Jobs: Docker Release ---

job "build-latest" {
  get "git" "pikoci_master" {
    trigger = true
  }
  task "docker-build-push-latest" {
    run "exec" {
      path = "/bin/sh"
      args = [
        "-ec",
        <<-EOT
        cd ${var.git_name}

        echo "${var.ghcr_token}" | docker login -u "${var.ghcr_username}" --password-stdin

        docker buildx create --use --name pikoci-builder 2>/dev/null || docker buildx use pikoci-builder
        docker buildx build --platform linux/amd64,linux/arm64 -t xescugc/pikoci:latest --push .
        EOT
      ]
    }
  }
  ensure {
    task "docker-prune" {
      run "exec" {
        path = "/bin/sh"
        args = ["-ec", "docker buildx prune -f && docker image prune -f"]
      }
    }
  }
}

job "deploy" {
  get "git" "pikoci_master" {
    trigger = true
    passed  = ["build-latest"]
  }
  task "build-and-replace" {
    run "docker" {
      image = "golang:1.25.1"
      cmd   = <<-EOT
        cd ${var.git_name}
        GOOS=linux GOARCH=arm64 go build -buildvcs=false -o /tmp/pikoci-new .
        mv /tmp/pikoci-new /hostbin/pikoci
      EOT
      args = [
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
        "-v", "/usr/local/bin:/hostbin",
      ]
    }
  }
  task "restart" {
    run "exec" {
      path = "/bin/sh"
      args = [
        "-ec",
        <<-EOT
        kill -QUIT $(pidof pikoci)
        EOT
      ]
    }
  }
}

job "deploy-website" {
  get "git" "pikoci_com" {
    trigger = true
  }
  task "copy-to-server" {
    run "exec" {
      path = "/bin/sh"
      args = [
        "-ec",
        <<-EOT
        mkdir -p /var/www/pikoci.com
        cp pikoci.com/index.html /var/www/pikoci.com/index.html
        EOT
      ]
    }
  }
}

job "build-release" {
  get "git" "pikoci_tag" {
    trigger = true
  }
  task "docker-build-push-tag" {
    run "exec" {
      path = "/bin/sh"
      args = [
        "-ec",
        <<-EOT
        cd ${var.git_name}
        TAG=$(git describe --tags --exact-match)

        echo "${var.ghcr_token}" | docker login -u "${var.ghcr_username}" --password-stdin

        docker buildx create --use --name pikoci-builder 2>/dev/null || docker buildx use pikoci-builder
        docker buildx build --platform linux/amd64,linux/arm64 -t xescugc/pikoci:$TAG --push .
        EOT
      ]
    }
  }
  ensure {
    task "docker-prune" {
      run "exec" {
        path = "/bin/sh"
        args = ["-ec", "docker buildx prune -f && docker image prune -f"]
      }
    }
  }
}

# --- Resources ---

resource_type "git" {
  source = "pikoci://git"
}

resource "git" "pikoci_pr" {
  params {
    url   = var.git_url
    name  = var.git_name
    pr    = true
    token = var.github_token
  }
}

resource "git" "pikoci_master" {
  params {
    url    = var.git_url
    name   = var.git_name
    branch = "master"
  }
}

resource "git" "pikoci_com" {
  params {
    url    = "https://github.com/pikoci/pikoci.com"
    name   = "pikoci.com"
    branch = "main"
  }
}

resource "git" "pikoci_tag" {
  params {
    url   = var.git_url
    name  = var.git_name
    token = var.github_token
    tag   = true
  }
}

resource_type "github-check" {
  source = "pikoci://github-check"
}

resource "github-check" "ci" {
  params {
    app_id          = var.github_app_id
    installation_id = var.github_app_installation_id
    private_key     = var.pikoci_github_app_pem
    repository      = "xescugc/pikoci"
  }
}

# --- Services ---

service_type "mariadb" {
  source = "pikoci://mariadb"
}

service_type "postgresql" {
  source = "pikoci://postgresql"
}

service_type "nats" {
  source = "pikoci://nats"
}

service_type "rabbitmq" {
  source = "pikoci://rabbitmq"
}

service_type "kafka" {
  source = "pikoci://kafka"
}

service_type "vault" {
  source = "pikoci://vault"
}

# --- Secrets and Variables ---

secret_type "env" {
  source = "pikoci://file"
  format = "env"
  path   = "/etc/pikoci/pikoci.env"
}

secret_type "pikoci_github_pem" {
  source = "pikoci://file"
  format = "raw"
  path   = "/etc/pikoci/pikoci_github_app.pem"
}

variable "git_url" {
  type    = string
  default = "https://github.com/xescugc/pikoci"
}

variable "git_name" {
  type    = string
  default = "pikoci"
}

variable "github_token" {
  type = string
  secret "env" {
    key = "GITHUB_TOKEN"
  }
}

variable "github_app_id" {
  type = string
  secret "env" {
    key = "GITHUB_APP_ID"
  }
}

variable "github_app_installation_id" {
  type = string
  secret "env" {
    key = "GITHUB_APP_INSTALLATION_ID"
  }
}

variable "pikoci_github_app_pem" {
  type = string
  secret "pikoci_github_pem" {
    key = "content"
  }
}

variable "ghcr_username" {
  type = string
  secret "env" {
    key = "GHCR_USERNAME"
  }
}

variable "ghcr_token" {
  type = string
  secret "env" {
    key = "GHCR_TOKEN"
  }
}
