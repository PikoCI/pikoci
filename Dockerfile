FROM --platform=$BUILDPLATFORM golang:1.25.1 AS builder
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags "-X github.com/pikoci/pikoci/cmd.Version=$(git describe --tags --abbrev=0 2>/dev/null || echo dev) -X github.com/pikoci/pikoci/cmd.Commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" \
    -o /pikoci .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates git jq curl openssl docker-cli graphviz tini
COPY --from=builder /pikoci /usr/local/bin/pikoci

# tini is PID 1 so that orphaned processes get reaped.
#
# Resource checks run shell scripts that spawn git, curl and their own transport
# helpers. When one of those outlives its parent — a clone still running when the
# script is killed, a helper that outlives the git that started it — the kernel
# reparents it to PID 1, and reaping it becomes PID 1's job. pikoci waits on the
# processes it starts itself, but it never adopted a duty to wait on strangers,
# so as PID 1 it accumulated them: 9,000 defunct git processes in three days on
# one deployment, until the container hit its pids cgroup limit and every
# subsequent fork failed with EAGAIN. The symptom is the whole server appearing
# to hang, reporting "can't fork: Resource temporarily unavailable".
#
# An init rather than a reaper goroutine inside pikoci: a process that calls
# wait4(-1) races with os/exec, which is waiting on specific pids of its own, and
# whichever wins steals the other's exit status. Keeping the two jobs in separate
# processes is why init exists.
ENTRYPOINT ["/sbin/tini", "--", "pikoci"]
