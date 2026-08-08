# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the driver-manager
RUN CGO_ENABLED=0 GOOS=linux go build -o driver-manager ./services/driver-manager/

# Runtime stage
FROM alpine:3.19

# su-exec drops privileges without a setuid binary, so it still works under
# `security_opt: [no-new-privileges:true]` (that flag blocks *gaining* privileges
# via execve; a root process dropping to another uid is unaffected).
RUN apk --no-cache add ca-certificates docker wget su-exec && \
    addgroup -S openedge && adduser -S -G openedge openedge

WORKDIR /app

# Copy the binary from builder
COPY --from=builder --chown=openedge:openedge /app/driver-manager .

# ---------------------------------------------------------------------------
# Docker socket access and the residual risk that remains
# ---------------------------------------------------------------------------
# driver-manager spawns the per-gateway driver containers through
# /var/run/docker.sock, so it must be able to talk to that socket. The socket's
# group id is a property of the HOST (999 on Debian/Ubuntu, 0 on Docker Desktop
# and rootless Docker, something else elsewhere), so it cannot be baked in with a
# build-time `USER`: a hardcoded gid would silently lose socket access on any
# host that numbers its docker group differently, and every gateway would fail
# to start.
#
# Instead the entrypoint starts as root, reads the gid off the mounted socket,
# joins `openedge` to that group and immediately drops to the unprivileged
# `openedge` user. The long-running process is therefore never uid 0.
#
# RESIDUAL RISK — deliberately not solved here: access to the Docker API *is*
# host root, whatever uid holds it. `:ro` on the bind mount only makes the socket
# inode read-only; the API served over it is fully read-write, so anything that
# can reach it can `docker run --privileged -v /:/host` and own the host. Running
# as a non-root uid is defence in depth against a container-local escape, not a
# containment boundary. Closing it properly requires either
#   * a docker-socket-proxy (e.g. tecnativa/docker-socket-proxy) in front of the
#     socket, allowlisting only the containers/images/networks endpoints
#     driver-manager actually uses, or
#   * rootless Docker on the host.
# Both are deployment-level changes and are out of scope for this image.
RUN printf '%s\n' \
    '#!/bin/sh' \
    'set -e' \
    'SOCK=/var/run/docker.sock' \
    'if [ -S "$SOCK" ]; then' \
    '    DOCKER_GID=$(stat -c %g "$SOCK" 2>/dev/null || echo "")' \
    '    if [ -n "$DOCKER_GID" ] && [ "$DOCKER_GID" != "0" ]; then' \
    '        if ! getent group "$DOCKER_GID" >/dev/null 2>&1; then' \
    '            addgroup -g "$DOCKER_GID" dockersock 2>/dev/null || true' \
    '        fi' \
    '        DOCKER_GROUP=$(getent group "$DOCKER_GID" | cut -d: -f1)' \
    '        [ -n "$DOCKER_GROUP" ] && addgroup openedge "$DOCKER_GROUP" 2>/dev/null || true' \
    '    elif [ "$DOCKER_GID" = "0" ]; then' \
    '        # Docker Desktop / rootless: socket is root-group owned. Adding the' \
    '        # service account to gid 0 is the only way to keep it working; the' \
    '        # process is still a non-root uid.' \
    '        addgroup openedge root 2>/dev/null || true' \
    '    fi' \
    'else' \
    '    echo "[driver-manager] WARNING: $SOCK not mounted — cannot spawn driver containers" >&2' \
    'fi' \
    'exec su-exec openedge "$@"' \
    > /usr/local/bin/drop-privs.sh && chmod 0755 /usr/local/bin/drop-privs.sh

HEALTHCHECK --interval=30s --timeout=10s --start-period=20s --retries=3 \
  CMD wget -qO- http://localhost:8081/health || exit 1

ENTRYPOINT ["/usr/local/bin/drop-privs.sh"]
CMD ["./driver-manager"]
