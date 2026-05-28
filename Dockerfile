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
RUN apk add --no-cache ca-certificates git jq curl openssl docker-cli
COPY --from=builder /pikoci /usr/local/bin/pikoci
ENTRYPOINT ["pikoci"]
