# Advanced path only: prefer the host binary so tmux and local AI CLIs share
# the same environment. See docs/install-daemon.md.
FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o mewla ./cmd/mewla/

FROM alpine:3.21
RUN apk add --no-cache tmux
COPY --from=builder /build/mewla /usr/local/bin/mewla
RUN ln -s mewla /usr/local/bin/zen
EXPOSE 9876
ENTRYPOINT ["mewla"]
