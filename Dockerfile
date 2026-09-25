# The base image comes from the internal registry's mirror; a build outside the
# network passes the public one:
#
#   --build-arg GO_IMAGE=docker.io/library/golang:1.27-alpine
ARG GO_IMAGE=zot.metapatrol.local/library/golang:1.27-alpine

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

# Distroless while the bridge only speaks HTTP. Importing pull requests
# (phase 2) needs the git CLI, and the runtime image changes with it.
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /forgejo-github-bridge /forgejo-github-bridge
EXPOSE 8080
ENTRYPOINT ["/forgejo-github-bridge"]
