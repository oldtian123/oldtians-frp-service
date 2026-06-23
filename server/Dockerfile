# Build stage -----------------------------------------------------------------
FROM golang:1.25-alpine AS build

WORKDIR /src

# Cache modules first.
COPY go.mod go.sum* ./
RUN go mod download || true

# Copy the rest of the server module and build a static, CGO-free binary.
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w -X main.version=docker" \
    -o /out/ofs-control-api ./cmd/control-api

# Runtime stage ---------------------------------------------------------------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget && \
    adduser -D -u 10001 ofs

WORKDIR /app
COPY --from=build /out/ofs-control-api /usr/local/bin/ofs-control-api

# Data directory for the SQLite database (mount a volume here).
RUN mkdir -p /app/data && chown -R ofs:ofs /app
USER ofs

EXPOSE 8088

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8088/healthz || exit 1

ENTRYPOINT ["ofs-control-api"]
CMD ["-config", "/app/config.yaml"]
