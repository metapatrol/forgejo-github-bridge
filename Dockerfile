# syntax=docker/dockerfile:1

# Public base images, so that `docker build .` works anywhere. A build that
# should pull through a registry mirror passes its own:
#
#   --build-arg NODE_IMAGE=registry.example.com/library/node:24-alpine
#   --build-arg GO_IMAGE=registry.example.com/library/golang:1.27-alpine
#   --build-arg RUNTIME_IMAGE=registry.example.com/library/alpine:3.24
ARG NODE_IMAGE=docker.io/library/node:24-alpine
ARG GO_IMAGE=docker.io/library/golang:1.27-alpine
ARG RUNTIME_IMAGE=docker.io/library/alpine:3.24

# ── Console web application ────────────────────────────────────────────────
FROM ${NODE_IMAGE} AS ui-builder

WORKDIR /src/ui
COPY ui/package.json ui/package-lock.json ui/.npmrc ./
# @metapatrol/* come from a registry that wants a token: pass one with
# read:package scope as the secret npm_token, and a CA the build host does not
# trust as platform_ca (docker build --secret id=npm_token,src=...).
RUN --mount=type=secret,id=npm_token --mount=type=secret,id=platform_ca \
    if [ -s /run/secrets/platform_ca ]; then export NODE_EXTRA_CA_CERTS=/run/secrets/platform_ca; fi; \
    NPM_TOKEN="$(cat /run/secrets/npm_token 2>/dev/null)" npm ci --no-audit --no-fund
COPY ui/ ./
# The API client is generated from the specification; the build writes to
# ../internal/ui/dist/console.
COPY api/openapi-v1.yaml ../api/openapi-v1.yaml
RUN npm run build

# ── The binary ─────────────────────────────────────────────────────────────
FROM ${GO_IMAGE} AS builder

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=ui-builder /src/internal/ui/dist/console/ internal/ui/dist/console/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtAt=${BUILT_AT}" \
    -o /forgejo-github-bridge ./cmd/forgejo-github-bridge

# Alpine rather than distroless: importing pull requests runs the git CLI
# (docs/design.md §18). Nothing else is added, and the process runs as the same
# unprivileged user distroless would use.
FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache git ca-certificates \
 && mkdir -p /var/cache/forgejo-github-bridge \
 && chown 65532:65532 /var/cache/forgejo-github-bridge
COPY --from=builder /forgejo-github-bridge /forgejo-github-bridge
USER 65532:65532
# The git cache belongs to the process, so that a volume mounted over it
# starts out writable; git needs a home.
ENV HOME=/tmp
VOLUME /var/cache/forgejo-github-bridge
# Probes and metrics; the console, when it is turned on.
EXPOSE 8080 8090
ENTRYPOINT ["/forgejo-github-bridge"]
