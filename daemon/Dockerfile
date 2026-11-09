# syntax=docker/dockerfile:1
# Advanced path only: prefer the host binary so agents and their CLIs share the
# person's own environment. See docs/install-daemon.md.
#
# Build from the repository root (the engine's libraries are built from the
# pinned sources the scripts there describe):
#
#   docker build -f daemon/Dockerfile -t mewla .
#
# The daemon carries the session host's terminal engine (build tag ghosttyvt):
# libghostty-vt is built from its signed, pinned release with the pinned Zig and
# linked statically against musl with `zig cc`, the same way
# scripts/build-daemon-linux.sh builds the Linux release binaries. The image
# needs no tmux. Unlike a release binary it never updates itself; rebuild the
# image instead.

FROM golang:1.26-alpine AS builder
RUN apk add --no-cache bash curl python3 tar xz
WORKDIR /src
COPY daemon/go.mod daemon/go.sum daemon/
RUN cd daemon && go mod download
COPY . .
ARG TARGETARCH
ENV MEWLA_TOOL_CACHE=/tools
RUN set -eu; \
    case "${TARGETARCH:-$(go env GOARCH)}" in \
      amd64) goarch=amd64; zigarch=x86_64 ;; \
      arm64) goarch=arm64; zigarch=aarch64 ;; \
      *) echo "unsupported architecture ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    ./scripts/build-libghostty-desktop.sh --out /libs --targets "linux_${goarch}_musl"; \
    . scripts/lib/zig.sh; \
    zig="$(mewla_resolve_zig app/modules/terminal-vt/native.lock.json /tools)"; \
    cd daemon; \
    ZIG_GLOBAL_CACHE_DIR=/tmp/zig-cc ZIG_LOCAL_CACHE_DIR=/tmp/zig-cc \
    CGO_ENABLED=1 CC="$zig cc -target ${zigarch}-linux-musl" \
    CGO_CFLAGS="-I/libs/include" CGO_LDFLAGS="-L/libs/linux_${goarch}_musl" \
      go build -trimpath -buildvcs=false -tags ghosttyvt,netgo,osusergo \
      -ldflags="-s -w -linkmode external -extldflags -static" -o /out/mewla ./cmd/mewla/; \
    go version -m /out/mewla | grep -Eq '^[[:space:]]+build[[:space:]]+-tags=([^ ]*,)?ghosttyvt(,|$)'

FROM alpine:3.21
COPY --from=builder /out/mewla /usr/local/bin/mewla
# The image's own session host must be able to run: fail the build otherwise.
RUN mewla version
EXPOSE 9876
ENTRYPOINT ["mewla"]
