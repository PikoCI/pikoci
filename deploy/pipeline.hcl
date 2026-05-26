# --- Jobs: CI ---

job "lint" {
  get "git" "pikoci_pr" {
    trigger = true
  }
  put "github-check" "ci" { status = "in_progress" }
  task "install-jq" {
    run "docker" {
      image = var.go_image
      cmd   = "apt-get update -qq && apt-get install -qq -y jq && cp /usr/bin/jq /pikoci-tools/ && cp /usr/lib/*/libjq.so* /usr/lib/*/libonig.so* /pikoci-tools/ 2>/dev/null; true"
      args  = [
        "-v", "pikoci-tools:/pikoci-tools",
      ]
    }
  }
  task "make" {
    run "docker" {
      image = var.go_image
      cmd   = "export PATH=/pikoci-tools:$PATH LD_LIBRARY_PATH=/pikoci-tools:$LD_LIBRARY_PATH && cd ${var.git_name} && make lint"
      args  = [
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
        "-v", "pikoci-tools:/pikoci-tools",
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
  task "install-jq" {
    run "docker" {
      image = var.go_image
      cmd   = "apt-get update -qq && apt-get install -qq -y jq && cp /usr/bin/jq /pikoci-tools/ && cp /usr/lib/*/libjq.so* /usr/lib/*/libonig.so* /pikoci-tools/ 2>/dev/null; true"
      args  = [
        "-v", "pikoci-tools:/pikoci-tools",
      ]
    }
  }
  task "make" {
    run "docker" {
      image = var.go_image
      cmd   = "export PATH=/pikoci-tools:$PATH LD_LIBRARY_PATH=/pikoci-tools:$LD_LIBRARY_PATH && cd ${var.git_name} && make test-mock"
      args  = [
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
        "-v", "pikoci-tools:/pikoci-tools",
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
  task "install-jq" {
    run "docker" {
      image = "ghcr.io/xescugc/pikoci-integration:latest"
      cmd   = "apt-get update -qq && apt-get install -qq -y jq && cp /usr/bin/jq /pikoci-tools/ && cp /usr/lib/*/libjq.so* /usr/lib/*/libonig.so* /pikoci-tools/ 2>/dev/null; true"
      args  = [
        "-v", "pikoci-tools:/pikoci-tools",
      ]
    }
  }
  task "make" {
    run "docker" {
      image = "ghcr.io/xescugc/pikoci-integration:latest"
      cmd   = <<-EOT
        export PATH=/pikoci-tools:$PATH LD_LIBRARY_PATH=/pikoci-tools:$LD_LIBRARY_PATH
        cd ${var.git_name}
        cp /usr/local/bin/geckodriver integration/vendor/geckodriver
        make test-integration
      EOT
      args  = [
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
        "-v", "pikoci-tools:/pikoci-tools",
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
  concurrency = 1
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

  task "install-jq" {
    run "docker" {
      image = var.go_image
      cmd   = "apt-get update -qq && apt-get install -qq -y jq && cp /usr/bin/jq /pikoci-tools/ && cp /usr/lib/*/libjq.so* /usr/lib/*/libonig.so* /pikoci-tools/ 2>/dev/null; true"
      args  = [
        "--network=host",
        "-v", "pikoci-tools:/pikoci-tools",
      ]
    }
  }
  task "make" {
    run "docker" {
      image = var.go_image
      cmd   = "export PATH=/pikoci-tools:$PATH LD_LIBRARY_PATH=/pikoci-tools:$LD_LIBRARY_PATH && cd ${var.git_name} && make test-backends"
      args  = [
        "--network=host",
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
        "-v", "pikoci-tools:/pikoci-tools",
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

        echo "${var.ghcr_token}" | docker login ghcr.io -u "${var.ghcr_username}" --password-stdin

        docker buildx create --use --name pikoci-builder 2>/dev/null || docker buildx use pikoci-builder
        docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/pikoci/pikoci:latest --push .
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
  concurrency = 1
  get "git" "pikoci_master" {
    trigger = true
    passed  = ["build-latest"]
  }
  task "build-and-replace" {
    run "docker" {
      image = var.go_image
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
  on_success "exec" {
    path = "/bin/sh"
    args = ["-ec", "kill -QUIT $(pidof pikoci)"]
  }
}

job "deploy-docs" {
  get "git" "pikoci_master" {
    trigger = true
  }
  task "build-and-deploy" {
    run "exec" {
      path = "/bin/sh"
      args = [
        "-ec",
        <<-EOT
        cd ${var.git_name}
        python3 -m venv .venv
        .venv/bin/pip install --quiet mkdocs-material
        .venv/bin/mkdocs build --clean
        mkdir -p /var/www/docs.pikoci.com
        cp -a site/. /var/www/docs.pikoci.com/
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
        cp -a pikoci.com/. /var/www/pikoci.com/
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

        echo "${var.ghcr_token}" | docker login ghcr.io -u "${var.ghcr_username}" --password-stdin

        docker buildx create --use --name pikoci-builder 2>/dev/null || docker buildx use pikoci-builder
        docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/pikoci/pikoci:$TAG --push .
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

job "gh-release" {
  get "git" "pikoci_tag" {
    trigger = true
    passed  = ["build-release"]
  }
  task "build-and-upload" {
    run "docker" {
      image = var.go_image
      cmd   = <<-EOT
        cd ${var.git_name}
        TAG=$(git describe --tags --exact-match)

        # Build all platforms
        make release

        # Install gh CLI
        curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /usr/share/keyrings/githubcli-archive-keyring.gpg
        echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list
        apt-get update -qq && apt-get install -qq -y gh

        # Extract changelog for this version
        VERSION=$${TAG#v}
        BODY=$(sed -n "/^## \[$VERSION\]/,/^## \[/{/^## \[$VERSION\]/d;/^## \[/d;p;}" CHANGELOG.md)

        # Create release with changelog body and upload binaries
        GH_TOKEN="${var.github_token}" gh release create $TAG \
          --title "$TAG" \
          --notes "$BODY" \
          --repo PikoCI/pikoci \
          builds/linux-amd64 builds/linux-arm64 builds/darwin-amd64 builds/darwin-arm64 builds/windows-amd64
      EOT
      args = [
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
      ]
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
    repository      = "PikoCI/pikoci"
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

variable "go_image" {
  type    = string
  default = "golang:1.25.1"
}

variable "git_url" {
  type    = string
  default = "https://github.com/PikoCI/pikoci"
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
