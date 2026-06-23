# Dockerfile for ofs-gateway-agent (the internal jump-host client).
#
# The agent manages frpc as a child process, so this image BUNDLES frpc. Build
# context is the agent/ module directory.
#
# Build (amd64):
#   docker build -t oldtians-frp-service/ofs-gateway-agent ./agent
# Build for a different frp version / arch:
#   docker build --build-arg FRP_VERSION=0.61.1 --build-arg FRP_ARCH=arm64 \
#     -t oldtians-frp-service/ofs-gateway-agent ./agent
#
# Run (host networking is recommended so frpc and the health checks can reach
# other LAN machines, e.g. 192.168.1.10:5244):
#   docker run -d --name ofs-gateway-agent \
#     --network host --restart unless-stopped \
#     -v /etc/oldtians-frp-service:/etc/oldtians-frp-service \
#     oldtians-frp-service/ofs-gateway-agent
#
# Mount a config.yaml (and let state.yaml / frpc.toml be written) under
# /etc/oldtians-frp-service. To reach the local publish API from other hosts,
# set agent.listen_addr: "0.0.0.0" in the config.

# ---- frpc download stage ----------------------------------------------------
FROM alpine:3.20 AS frpc
ARG FRP_VERSION=0.61.1
# TARGETARCH is supplied automatically by buildx; default to amd64 for plain builds.
ARG TARGETARCH=amd64
ARG FRP_ARCH=${TARGETARCH}
RUN set -eux; \
    apk add --no-cache wget tar ca-certificates; \
    wget -O /tmp/frp.tgz "https://github.com/fatedier/frp/releases/download/v${FRP_VERSION}/frp_${FRP_VERSION}_linux_${FRP_ARCH}.tar.gz"; \
    tar -xzf /tmp/frp.tgz -C /tmp; \
    install -m 0755 "/tmp/frp_${FRP_VERSION}_linux_${FRP_ARCH}/frpc" /usr/local/bin/frpc; \
    /usr/local/bin/frpc --version

# ---- build stage ------------------------------------------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache modules first.
COPY go.mod go.sum* ./
RUN go mod download || true

# Build a static, CGO-free binary.
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w -X main.version=docker" \
    -o /out/ofs-gateway-agent ./cmd/gateway-agent

# ---- runtime stage ----------------------------------------------------------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget

COPY --from=frpc  /usr/local/bin/frpc     /usr/local/bin/frpc
COPY --from=build /out/ofs-gateway-agent  /usr/local/bin/ofs-gateway-agent

# Work dir holds config.yaml, the generated frpc.toml and state.yaml.
# Runs as root so it can write the mounted volume and exec frpc without
# permission friction (this is an internal jump-host tool).
RUN mkdir -p /etc/oldtians-frp-service
WORKDIR /etc/oldtians-frp-service

# Local publish API (set agent.listen_addr to 0.0.0.0 to reach it from the LAN).
EXPOSE 8899

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8899/healthz || exit 1

ENTRYPOINT ["ofs-gateway-agent"]
CMD ["-config", "/etc/oldtians-frp-service/config.yaml"]
