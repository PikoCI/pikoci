GOCACHE := $(shell go env GOCACHE)

.PHONY: help
help: Makefile ## This help dialog
	@IFS=$$'\n' ; \
	help_lines=(`grep -F -h "##" $(MAKEFILE_LIST) | grep -F -v grep -F | sed -e 's/\\$$//'`); \
	for help_line in $${help_lines[@]}; do \
		IFS=$$'#' ; \
		help_split=($$help_line) ; \
		help_command=`echo $${help_split[0]} | sed -e 's/^ *//' -e 's/ *$$//'` ; \
		help_info=`echo $${help_split[2]} | sed -e 's/^ *//' -e 's/ *$$//'` ; \
		printf "%-30s %s\n" $$help_command $$help_info ; \
	done

export GOCACHE

.PHONY: test-services-up
test-services-up: ## Start all external test services (DB, vault)
	@docker-compose -f docker/docker-compose.yml -f docker/develop.yml up -d mariadb postgresql vault

.PHONY: test-services-down
test-services-down: ## Stop all external test services
	@docker-compose -f docker/docker-compose.yml -f docker/develop.yml down -v --remove-orphans

.PHONY: dev-env-up
dev-env-up: ## Starts the development dependencies
	@docker-compose -f docker/docker-compose.yml -f docker/develop.yml up -d mariadb

.PHONY: nats-up
nats-up: ## Starts NATS for development
	@docker-compose -f docker/docker-compose.yml -f docker/develop.yml up -d nats

.PHONY: down
down: ## Stops the containers
	@docker-compose -f docker/docker-compose.yml -f docker/develop.yml down -v --remove-orphans

.PHONY: dserve
dserve: ## Serves the server
	@docker-compose -f docker/docker-compose.yml -f docker/develop.yml run --name pikoci --rm -p 4000:4000 pikoci go run . server

.PHONY: serve
serve: ## Serves the server
	@go run . server -p 4000 --log-level=debug --jwt-secret potato --concurrency=2 --users 'pepito:$$2a$$14$$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm,grillo:$$2a$$14$$SvWir17.jlXxiZfe0pJuDedznetc/HWKv43YPsQQNo6MJiuypS2q6' --pipeline-name=test --pipeline-config=./pikoci/testdata/cron.hcl

.PHONY: worker
worker: ## Starts a worker
	@docker-compose -f docker/docker-compose.yml -f docker/develop.yml run --rm worker go run . worker

.PHONY: db-cli
db-cli: ## Locally connects to the DB
	@docker-compose -f docker/docker-compose.yml -f docker/develop.yml exec mariadb mariadb -uroot -proot123

.PHONY: proto
proto: ## Generates protobuf Go code
	@protoc --go_out=. --go_opt=module=github.com/pikoci/pikoci --go-grpc_out=. --go-grpc_opt=module=github.com/pikoci/pikoci proto/worker/v1/worker.proto

.PHONY: gen
gen: ## Runs go generate
	@go generate ./...

.PHONY: lint
lint: ## Runs staticcheck linter
	GOFLAGS=-buildvcs=false go tool staticcheck ./...

.PHONY: test
test: test-mock test-http test-integration test-backends ## Runs all tests

.PHONY: test-mock
test-mock: ## Runs unit/mock tests (no services needed)
	go test ./... -timeout 120s -coverprofile=coverage.out

.PHONY: test-http
test-http: ## Runs HTTP API integration tests (no browser needed)
	go test -tags integration ./integration/http/... -v

.PHONY: test-integration
test-integration: ## Runs UI and backend integration tests (requires geckodriver + Xvfb + Firefox)
	@PIKOCI_TEST_DB_SYSTEMS=$${PIKOCI_TEST_DB_SYSTEMS:-mem,sqlite} \
	go test -tags integration ./integration/selenium/

.PHONY: test-backends
test-backends: ## Runs integration tests with all backends (requires test-services-up)
	@PIKOCI_TEST_DB_SYSTEMS=mem,sqlite,mysql,postgresql \
	PIKOCI_TEST_VAULT=1 \
	PIKOCI_TEST_VAULT_ADDR=http://127.0.0.1:8200 \
	go test -tags integration ./integration/backends/... -coverprofile=coverage-backends.out

.PHONY: tag
tag: ## Tag a release: make tag SEMVER=major|minor|patch
ifndef SEMVER
	$(error SEMVER is required. Usage: make tag SEMVER=major|minor|patch)
endif
ifeq ($(filter $(SEMVER),major minor patch),)
	$(error SEMVER must be one of: major, minor, patch)
endif
	$(eval CURRENT := $(shell git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0))
	$(eval MAJOR := $(shell echo $(CURRENT) | sed 's/^v//' | cut -d. -f1))
	$(eval MINOR := $(shell echo $(CURRENT) | sed 's/^v//' | cut -d. -f2))
	$(eval PATCH := $(shell echo $(CURRENT) | sed 's/^v//' | cut -d. -f3))
ifeq ($(SEMVER),major)
	$(eval NEXT := v$(shell echo $$(($(MAJOR)+1))).0.0)
else ifeq ($(SEMVER),minor)
	$(eval NEXT := v$(MAJOR).$(shell echo $$(($(MINOR)+1))).0)
else ifeq ($(SEMVER),patch)
	$(eval NEXT := v$(MAJOR).$(MINOR).$(shell echo $$(($(PATCH)+1))))
endif
	$(eval NEXT_BARE := $(shell echo $(NEXT) | sed 's/^v//'))
	sed -i 's/^## \[Unreleased\]$$/## [Unreleased]\n\n## [$(NEXT_BARE)] - $(shell date +%Y-%m-%d)/' CHANGELOG.md
	git add CHANGELOG.md
	git commit -m "Release $(NEXT)"
	git tag -a $(NEXT) -m "Release $(NEXT)"
	git push origin master $(NEXT)
	@echo ""
	@echo "===== Released $(NEXT) ====="

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

temp = $(subst /, ,$@)
os = $(word 1, $(temp))
arch = $(word 2, $(temp))

.PHONY: release $(PLATFORMS)
release: $(PLATFORMS) ## Creates the bin on the ./builds/

VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD)
LDFLAGS := -X github.com/pikoci/pikoci/cmd.Version=$(VERSION) -X github.com/pikoci/pikoci/cmd.Commit=$(COMMIT)

$(PLATFORMS):
	$(eval SUFFIX := $(if $(filter windows,$(os)),.exe,))
	GOOS=$(os) GOARCH=$(arch) go build -ldflags "$(LDFLAGS)" -o ./builds/pikoci-$(os)-$(arch)$(SUFFIX) .
