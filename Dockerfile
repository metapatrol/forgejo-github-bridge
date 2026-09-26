# Public base images, so that `docker build .` works anywhere. A build that
# should pull through a registry mirror passes its own:
#
#   --build-arg GO_IMAGE=registry.example.com/library/golang:1.27-alpine
#   --build-arg RUNTIME_IMAGE=registry.example.com/library/alpine:3.24
ARG GO_IMAGE=docker.io/library/golang:1.27-alpine
ARG RUNTIME_IMAGE=docker.io/library/alpine:3.24

FROM ${GO_IMAGE} AS builder

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.builtAt=${BUILT_AT}" \
    -o /forgejo-github-bridge ./cmd/forgejo-github-bridge

# Alpine rather than distroless: importing pull requests runs the git CLI
# (docs/design.md §18). Nothing else is added, and the process runs as the same
# unprivileged user distroless would use.
FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache git ca-certificates
COPY --from=builder /forgejo-github-bridge /forgejo-github-bridge
USER 65532:65532
ENV HOME=/tmp
EXPOSE 8080
ENTRYPOINT ["/forgejo-github-bridge"]
