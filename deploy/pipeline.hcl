# --- Jobs: CI ---

job "backend" {
  get "git" "pikoci_pr" {
    trigger = true
  }
  notify "github-check" "ci" { status = "in_progress" }
  task "install-tools" {
    run "docker" {
      image = var.go_image
      cmd   = <<-EOT
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -qq && apt-get install -qq -y jq graphviz
        cp /usr/bin/jq /pikoci-tools/
        cp /usr/lib/*/libjq.so* /usr/lib/*/libonig.so* /pikoci-tools/ 2>/dev/null; true
        if [ ! -f /pikoci-tools/dot ]; then
          cp /usr/bin/dot /pikoci-tools/
          LIBDIR=$(ls -d /usr/lib/*-linux-gnu 2>/dev/null | head -1)
          cp "$LIBDIR"/lib*.so* /pikoci-tools/ 2>/dev/null || true
          mkdir -p /pikoci-tools/graphviz
          cp "$LIBDIR"/graphviz/*.so* "$LIBDIR"/graphviz/config* /pikoci-tools/graphviz/ 2>/dev/null || true
          dot -c 2>/dev/null || true
          sed -i "s|$LIBDIR/graphviz|/pikoci-tools/graphviz|g" /pikoci-tools/graphviz/config* 2>/dev/null || true
        fi
        if [ ! -f /pikoci-tools/codecov ]; then
          ARCH=$(dpkg --print-architecture)
          if [ "$ARCH" = "arm64" ]; then
            curl -fsSL https://cli.codecov.io/latest/linux-arm64/codecov -o /pikoci-tools/codecov
          else
            curl -fsSL https://cli.codecov.io/latest/linux/codecov -o /pikoci-tools/codecov
          fi
          chmod +x /pikoci-tools/codecov
        fi
      EOT
      args  = [
        "-v", "pikoci-tools:/pikoci-tools",
      ]
    }
  }
  in_parallel {
    limit = 2
    task "lint" {
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
    task "test-mock" {
      run "docker" {
        image = var.go_image
        cmd   = "export PATH=/pikoci-tools:$PATH LD_LIBRARY_PATH=/pikoci-tools:$LD_LIBRARY_PATH GVBINDIR=/pikoci-tools/graphviz && cd ${var.git_name} && make test-mock"
        args  = [
          "-v", "pikoci-go-mod:/go/pkg/mod",
          "-v", "pikoci-build:/root/.cache/go-build",
          "-v", "pikoci-tools:/pikoci-tools",
        ]
      }
    }
    task "test-http" {
      run "docker" {
        image = var.go_image
        cmd   = "export PATH=/pikoci-tools:$PATH LD_LIBRARY_PATH=/pikoci-tools:$LD_LIBRARY_PATH GVBINDIR=/pikoci-tools/graphviz && cd ${var.git_name} && make test-http"
        args  = [
          "-v", "pikoci-go-mod:/go/pkg/mod",
          "-v", "pikoci-build:/root/.cache/go-build",
          "-v", "pikoci-tools:/pikoci-tools",
        ]
      }
    }
  }
  task "upload-coverage" {
    run "docker" {
      image = var.go_image
      cmd   = <<-EOT
        export PATH=/pikoci-tools:$PATH LD_LIBRARY_PATH=/pikoci-tools:$LD_LIBRARY_PATH
        cd ${var.git_name}
        codecov upload-process \
          --token "${var.codecov_token}" \
          --file coverage.out \
          --flag unit \
          --git-service github \
          --slug PikoCI/pikoci \
          --sha "$GET_PIKOCI_PR_REF" \
          --branch "$(git symbolic-ref --short HEAD 2>/dev/null || echo pr)"
      EOT
      args = [
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
        "-v", "pikoci-tools:/pikoci-tools",
      ]
    }
  }
  on_success {
    notify "github-check" "ci" { conclusion = "success" }
  }
  on_failure {
    notify "github-check" "ci" { conclusion = "failure" }
  }
  on_cancel {
    notify "github-check" "ci" { conclusion = "cancelled" }
  }
}

job "frontend" {
  get "git" "pikoci_pr" {
    trigger = true
  }
  notify "github-check" "ci" { status = "in_progress" }
  task "install-tools" {
    run "docker" {
      image = "node:20-slim"
      cmd   = <<-EOT
        if [ ! -f /pikoci-js-tools/node_modules/.bin/oxlint ]; then
          cd /pikoci-js-tools && npm install oxlint
        fi
      EOT
      args = ["-v", "pikoci-js-tools:/pikoci-js-tools"]
    }
  }
  in_parallel {
    limit = 2
    task "lint" {
      run "docker" {
        image = "node:20-slim"
        cmd   = "export PATH=/pikoci-js-tools/node_modules/.bin:$PATH && cd ${var.git_name} && oxlint --deny-warnings pikoci/transport/http/assets/js/app/"
        args  = ["-v", "pikoci-js-tools:/pikoci-js-tools"]
      }
    }
    task "test" {
      run "docker" {
        image = "node:20-slim"
        cmd   = "cd ${var.git_name} && node --import ./test/js/setup.mjs --test test/js/utils.test.mjs test/js/state.test.mjs test/js/api.test.mjs test/js/components.test.mjs test/js/hooks.test.mjs test/js/toast.test.mjs"
        args  = ["-v", "pikoci-js-tools:/pikoci-js-tools"]
      }
    }
  }
  on_success {
    notify "github-check" "ci" { conclusion = "success" }
  }
  on_failure {
    notify "github-check" "ci" { conclusion = "failure" }
  }
  on_cancel {
    notify "github-check" "ci" { conclusion = "cancelled" }
  }
}

job "test-integration" {
  get "git" "pikoci_pr" {
    trigger = true
  }
  notify "github-check" "ci" { status = "in_progress" }

  service "keycloak" {
    version        = "26.0"
    port           = "8180"
    admin_user     = "admin"
    admin_password = "admin"
    realm_json     = "pikoci/integration/fixtures/keycloak/pikoci-realm.json"
  }

  task "install-tools" {
    run "docker" {
      image = "ghcr.io/xescugc/pikoci-integration:latest"
      cmd   = <<-EOT
        apt-get update -qq && apt-get install -qq -y jq
        cp /usr/bin/jq /pikoci-tools/
        cp /usr/lib/*/libjq.so* /usr/lib/*/libonig.so* /pikoci-tools/ 2>/dev/null; true
        if [ ! -f /pikoci-tools/codecov ]; then
          ARCH=$(dpkg --print-architecture)
          if [ "$ARCH" = "arm64" ]; then
            curl -fsSL https://cli.codecov.io/latest/linux-arm64/codecov -o /pikoci-tools/codecov
          else
            curl -fsSL https://cli.codecov.io/latest/linux/codecov -o /pikoci-tools/codecov
          fi
          chmod +x /pikoci-tools/codecov
        fi
      EOT
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
        export KEYCLOAK_URL=http://127.0.0.1:8180
        cd ${var.git_name}
        cp /usr/local/bin/geckodriver integration/vendor/geckodriver
        make test-integration
      EOT
      args  = [
        "--network=host",
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
        "-v", "pikoci-tools:/pikoci-tools",
      ]
    }
  }
  on_success {
    notify "github-check" "ci" { conclusion = "success" }
  }
  on_failure {
    notify "github-check" "ci" { conclusion = "failure" }
  }
  on_cancel {
    notify "github-check" "ci" { conclusion = "cancelled" }
  }
}

job "test-backends" {
  concurrency = 1
  get "git" "pikoci_pr" {
    trigger = true
    passed  = ["backend", "frontend", "test-integration"]
  }

  notify "github-check" "ci" { status = "in_progress" }

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
  service "vault" {
    version    = "latest"
    port       = "8200"
    root_token = "test-root-token"
  }

  task "install-tools" {
    run "docker" {
      image = var.go_image
      cmd   = <<-EOT
        apt-get update -qq && apt-get install -qq -y jq
        cp /usr/bin/jq /pikoci-tools/
        cp /usr/lib/*/libjq.so* /usr/lib/*/libonig.so* /pikoci-tools/ 2>/dev/null; true
        if [ ! -f /pikoci-tools/codecov ]; then
          ARCH=$(dpkg --print-architecture)
          if [ "$ARCH" = "arm64" ]; then
            curl -fsSL https://cli.codecov.io/latest/linux-arm64/codecov -o /pikoci-tools/codecov
          else
            curl -fsSL https://cli.codecov.io/latest/linux/codecov -o /pikoci-tools/codecov
          fi
          chmod +x /pikoci-tools/codecov
        fi
      EOT
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
  task "upload-coverage" {
    run "docker" {
      image = var.go_image
      cmd   = <<-EOT
        export PATH=/pikoci-tools:$PATH LD_LIBRARY_PATH=/pikoci-tools:$LD_LIBRARY_PATH
        cd ${var.git_name}
        codecov upload-process \
          --token "${var.codecov_token}" \
          --file coverage-backends.out \
          --flag backends \
          --git-service github \
          --slug PikoCI/pikoci \
          --sha "$GET_PIKOCI_PR_REF" \
          --branch "$(git symbolic-ref --short HEAD 2>/dev/null || echo pr)"
      EOT
      args = [
        "--network=host",
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
        "-v", "pikoci-tools:/pikoci-tools",
      ]
    }
  }

  on_success {
    notify "github-check" "ci" { conclusion = "success" }
  }
  on_failure {
    notify "github-check" "ci" { conclusion = "failure" }
  }
  on_cancel {
    notify "github-check" "ci" { conclusion = "cancelled" }
  }
}

# --- Jobs: Docker Release ---

job "build-latest" {
  concurrency    = 1
  serial_groups  = ["docker-build"]
  get "git" "pikoci_master" {
    trigger = true
  }
  task "docker-build-push-latest" {
    run "shell" {
      cmd = <<-EOT
        cd ${var.git_name}

        echo "${var.ghcr_token}" | docker login ghcr.io -u "${var.ghcr_username}" --password-stdin

        docker buildx create --use --name pikoci-builder 2>/dev/null || docker buildx use pikoci-builder
        docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/pikoci/pikoci:latest --push .
      EOT
    }
  }
  ensure {
    task "docker-prune" {
      run "shell" {
        cmd = "docker buildx prune -f && docker image prune -f"
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
        GOOS=linux GOARCH=arm64 go build -buildvcs=false -ldflags "-X github.com/pikoci/pikoci/cmd.Version=$(git describe --tags --abbrev=0 2>/dev/null || echo dev) -X github.com/pikoci/pikoci/cmd.Commit=$(git rev-parse --short HEAD)" -o /tmp/pikoci-new .
        mv /tmp/pikoci-new /hostbin/pikoci
      EOT
      args = [
        "-v", "pikoci-go-mod:/go/pkg/mod",
        "-v", "pikoci-build:/root/.cache/go-build",
        "-v", "/usr/local/bin:/hostbin",
      ]
    }
  }
  on_success "shell" {
    cmd = "pkill -QUIT -u pikoci pikoci || true"
  }
}

job "deploy-docs" {
  get "git" "pikoci_master" {
    trigger = true
  }
  task "build-and-deploy" {
    run "shell" {
      cmd = <<-EOT
        cd ${var.git_name}
        python3 -m venv .venv
        .venv/bin/pip install --quiet mkdocs-material
        .venv/bin/mkdocs build --clean
        rm -rf /var/www/docs.pikoci.com/*
        cp -a site/. /var/www/docs.pikoci.com/
      EOT
    }
  }
}

job "deploy-website" {
  get "git" "pikoci_com" {
    trigger = true
  }
  task "copy-to-server" {
    run "shell" {
      cmd = <<-EOT
        mkdir -p /var/www/pikoci.com
        rsync -a --exclude='.git' pikoci.com/ /var/www/pikoci.com/
      EOT
    }
  }
}

job "build-release" {
  concurrency    = 1
  serial_groups  = ["docker-build"]
  get "git" "pikoci_tag" {
    trigger = true
  }
  task "docker-build-push-tag" {
    run "shell" {
      cmd = <<-EOT
        cd ${var.git_name}
        TAG=$(git describe --tags --exact-match)

        echo "${var.ghcr_token}" | docker login ghcr.io -u "${var.ghcr_username}" --password-stdin

        docker buildx create --use --name pikoci-builder 2>/dev/null || docker buildx use pikoci-builder
        docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/pikoci/pikoci:$TAG --push .
      EOT
    }
  }
  ensure {
    task "docker-prune" {
      run "shell" {
        cmd = "docker buildx prune -f && docker image prune -f"
      }
    }
  }
}

job "publish-release" {
  get "git" "pikoci_tag" {
    trigger = true
    passed  = ["build-release"]
  }

  task "install-tools" {
    run "docker" {
      image = var.go_image
      cmd   = <<-EOT
        # Install gh CLI if not cached
        if [ ! -f /pikoci-tools/gh ]; then
          curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
            -o /usr/share/keyrings/githubcli-archive-keyring.gpg
          echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
            > /etc/apt/sources.list.d/github-cli.list
          apt-get update -qq && apt-get install -qq -y gh
          cp /usr/bin/gh /pikoci-tools/
        fi

        # Install nfpm if not cached
        if [ ! -f /pikoci-tools/nfpm ]; then
          go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
          cp $(go env GOPATH)/bin/nfpm /pikoci-tools/
        fi
      EOT
      args = [
        "-v", "pikoci-tools:/pikoci-tools",
        "-v", "pikoci-go-mod:/go/pkg/mod",
      ]
    }
  }

  in_parallel {
    limit = 2

    task "build-linux-amd64" {
      run "docker" {
        image = var.go_image
        cmd   = <<-EOT
          cd ${var.git_name}
          make linux/amd64
        EOT
        args = [
          "-v", "pikoci-go-mod:/go/pkg/mod",
          "-v", "pikoci-build:/root/.cache/go-build",
        ]
      }
    }

    task "build-linux-arm64" {
      run "docker" {
        image = var.go_image
        cmd   = <<-EOT
          cd ${var.git_name}
          make linux/arm64
        EOT
        args = [
          "-v", "pikoci-go-mod:/go/pkg/mod",
          "-v", "pikoci-build:/root/.cache/go-build",
        ]
      }
    }

    task "build-darwin-amd64" {
      run "docker" {
        image = var.go_image
        cmd   = <<-EOT
          cd ${var.git_name}
          make darwin/amd64
        EOT
        args = [
          "-v", "pikoci-go-mod:/go/pkg/mod",
          "-v", "pikoci-build:/root/.cache/go-build",
        ]
      }
    }

    task "build-darwin-arm64" {
      run "docker" {
        image = var.go_image
        cmd   = <<-EOT
          cd ${var.git_name}
          make darwin/arm64
        EOT
        args = [
          "-v", "pikoci-go-mod:/go/pkg/mod",
          "-v", "pikoci-build:/root/.cache/go-build",
        ]
      }
    }

    task "build-windows-amd64" {
      run "docker" {
        image = var.go_image
        cmd   = <<-EOT
          cd ${var.git_name}
          make windows/amd64
        EOT
        args = [
          "-v", "pikoci-go-mod:/go/pkg/mod",
          "-v", "pikoci-build:/root/.cache/go-build",
        ]
      }
    }
  }

  task "package-deb-rpm" {
    run "docker" {
      image = var.go_image
      cmd   = <<-EOT
        export PATH=/pikoci-tools:$PATH
        cd ${var.git_name}
        TAG=$GET_PIKOCI_TAG_TAG
        VERSION=$${TAG#v}

        # Package for each Linux architecture
        for ARCH in amd64 arm64; do
          cp builds/pikoci-linux-$ARCH ./pikoci-bin
          GOARCH=$ARCH VERSION=$VERSION nfpm pkg --packager deb --target builds/pikoci_$${VERSION}_$${ARCH}.deb
          GOARCH=$ARCH VERSION=$VERSION nfpm pkg --packager rpm --target builds/pikoci-$${VERSION}.$${ARCH}.rpm
          rm ./pikoci-bin
        done
      EOT
      args = [
        "-v", "pikoci-tools:/pikoci-tools",
      ]
    }
  }

  task "create-release" {
    run "docker" {
      image = var.go_image
      cmd   = <<-EOT
        export PATH=/pikoci-tools:$PATH
        cd ${var.git_name}
        TAG=$GET_PIKOCI_TAG_TAG
        VERSION=$${TAG#v}

        # Generate SHA256SUMS
        cd builds
        sha256sum pikoci-linux-* pikoci-darwin-* pikoci-windows-* *.deb *.rpm > SHA256SUMS
        cd ..

        # Extract changelog for this version
        CHANGELOG=$(awk '/^## \['"$VERSION"'\]/{found=1; next} found && /^## \[/{exit} found{print}' CHANGELOG.md | sed '/^$/d')
        if [ -z "$CHANGELOG" ]; then
          CHANGELOG="See https://github.com/PikoCI/pikoci/releases/tag/$TAG"
        fi

        # Create release if it doesn't already exist (retries reuse the existing one)
        if ! GH_TOKEN="${var.github_token}" gh release view $TAG --repo PikoCI/pikoci >/dev/null 2>&1; then
          GH_TOKEN="${var.github_token}" gh release create $TAG \
            --title "$TAG" \
            --notes "$CHANGELOG" \
            --repo PikoCI/pikoci
        fi

        # Upload artifacts (--clobber overwrites assets from partial retries)
        GH_TOKEN="${var.github_token}" gh release upload $TAG \
          --repo PikoCI/pikoci \
          --clobber \
          builds/pikoci-linux-* \
          builds/pikoci-darwin-* \
          builds/pikoci-windows-* \
          builds/*.deb \
          builds/*.rpm \
          builds/SHA256SUMS

        # Export changelog for Discord (truncate to 1800 chars for Discord's 2000 limit, escape newlines)
        DISCORD_CL=$(echo "$CHANGELOG" | head -c 1800)
        if [ "$${#DISCORD_CL}" -lt "$${#CHANGELOG}" ]; then
          DISCORD_CL="$${DISCORD_CL}..."
        fi
        ESCAPED=$(echo "$DISCORD_CL" | awk 'NR>1{printf "\\n"}{printf "%s",$$0}')
        printf '%s\n' "CHANGELOG=$ESCAPED" >> "$PIKOCI_OUTPUT"
      EOT
      args = [
        "-v", "pikoci-tools:/pikoci-tools",
      ]
    }
  }

  on_success {
    notify "discord" "announcements" {
      message = "🚀 **PikoCI $GET_PIKOCI_TAG_TAG** has been released!\n\n$TASK_CREATE_RELEASE_CHANGELOG\n\n🔗 https://github.com/PikoCI/pikoci/releases/tag/$GET_PIKOCI_TAG_TAG"
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

notification_type "github-check" {
  source = "pikoci://github-check"
}

notification_type "discord" {
  source = "pikoci://discord"
}

notification "github-check" "ci" {
  params {
    app_id          = var.github_app_id
    installation_id = var.github_app_installation_id
    private_key     = var.pikoci_github_app_pem
    repository      = "PikoCI/pikoci"
    base_url        = "https://${var.pikoci_domain}"
  }
}

notification "discord" "announcements" {
  params {
    webhook_url = var.discord_announcements_webhook
    base_url    = "https://${var.pikoci_domain}"
  }
}

# --- Services ---

service_type "mariadb" {
  source = "pikoci://mariadb"
}

service_type "postgresql" {
  source = "pikoci://postgresql"
}

service_type "vault" {
  source = "pikoci://vault"
}

service_type "keycloak" {
  params = ["version", "port", "admin_user", "admin_password", "realm_json"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-keycloak"
      docker rm -f $NAME 2>/dev/null || true
      docker run -d --name $NAME \
        -p $param_port:8080 \
        -e KC_BOOTSTRAP_ADMIN_USERNAME=$param_admin_user \
        -e KC_BOOTSTRAP_ADMIN_PASSWORD=$param_admin_password \
        quay.io/keycloak/keycloak:$param_version \
        start-dev --import-realm
      if [ -n "$param_realm_json" ] && [ -f "$param_realm_json" ]; then
        docker exec $NAME mkdir -p /opt/keycloak/data/import
        docker cp "$param_realm_json" $NAME:/opt/keycloak/data/import/realm.json
        docker restart $NAME
      fi
    EOT
    ]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "curl -sf http://127.0.0.1:$param_port/realms/pikoci"]
    interval = "3s"
    timeout  = "120s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-$BUILD_PIPELINE_NAME-$BUILD_JOB_NAME-keycloak"
      docker rm -f $NAME 2>/dev/null || true
    EOT
    ]
  }
}

# --- Secrets and Variables ---

secret_type "env" {
  source = "pikoci://file"
  path   = "/etc/pikoci/pikoci.env"
}

secret_type "pikoci_github_pem" {
  source = "pikoci://file"
  path   = "/etc/pikoci/pikoci_github_app.pem"
}

variable "pikoci_domain" {
  type = string
  secret "env" {
    key = "PIKOCI_DOMAIN"
  }
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

variable "codecov_token" {
  type = string
  secret "env" {
    key = "CODECOV_TOKEN"
  }
}

variable "discord_announcements_webhook" {
  type = string
  secret "env" {
    key = "DISCORD_ANNOUNCEMENTS_WEBHOOK"
  }
}
