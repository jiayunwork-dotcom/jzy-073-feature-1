# Build on the exact required base. Dependencies are vendored (vendor/),
# so this build needs no module proxy and succeeds fully offline.
FROM golang:1.22-alpine AS build

WORKDIR /src

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath \
        -ldflags="-s -w" -o /out/isa-service ./cmd/server

# Minimal runtime image. Busybox wget is used for the healthcheck, so no
# extra packages are needed.
FROM alpine:3.20
RUN adduser -D -u 10001 isa
COPY --from=build /out/isa-service /usr/local/bin/isa-service
USER isa

ENV GIN_MODE=release
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:8080/api/v1/atmosphere/point?altitude=0" || exit 1

ENTRYPOINT ["/usr/local/bin/isa-service"]
