# Tool versions — single source of truth for local and CI.
#
# Pinned here rather than in a CI image tag so that `make lint` and the lint job
# cannot drift apart: a version that lives only in a workflow is one a local run
# has no way of seeing.
#
# A full release tag, not a minor: the install script the lint job uses fetches a
# GitHub release, and there is no release called v2.13.
GOLANGCI_LINT_VERSION ?= v2.13.2
GITLEAKS_VERSION ?= v8.30.1
HELM_VERSION ?= v3.16.3
HELM_DOCS_VERSION ?= v1.14.2

# The database the integration tests run against. Overridable, so a developer
# with a PostgreSQL already running can point at it instead of a container. The
# major version is the one CloudNativePG runs here.
TEST_POSTGRES_IMAGE ?= postgres:18
TEST_POSTGRES_PORT ?= 55433
TEST_DATABASE_URL ?= postgres://postgres:test@localhost:$(TEST_POSTGRES_PORT)/bridge?sslmode=disable

BINARY := forgejo-github-bridge

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILT_AT ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.builtAt=$(BUILT_AT)

.PHONY: build test integration-test integration-db integration-stop lint fmt vet clean \
        build-binary build-all install-tools secret-scan secret-scan-history \
        helm-docs helm-lint helm-verify helm-package changelog

build:
	go build ./...

test:
	go test -v -count=1 ./...

# The store and the whole engine against a real PostgreSQL. Skipped by a plain
# `go test ./...`, which is deliberate: the unit tests have to stay runnable
# without a database, and these catch what only a real server can reject —
# SQL that parses in a reviewer's head and not in PostgreSQL's.
integration-test: integration-db
	BRIDGE_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" \
	go test ./tests/integration/... -v -count=1 -timeout 5m

# Start a throwaway PostgreSQL and wait for it. Idempotent: an existing
# container is reused rather than being an error.
integration-db:
	@docker inspect bridge-test-db >/dev/null 2>&1 || \
		docker run -d --rm --name bridge-test-db \
			-e POSTGRES_PASSWORD=test -e POSTGRES_DB=bridge \
			-p $(TEST_POSTGRES_PORT):5432 $(TEST_POSTGRES_IMAGE) >/dev/null
	@for i in $$(seq 1 30); do \
		docker exec bridge-test-db pg_isready -U postgres >/dev/null 2>&1 && exit 0; \
		sleep 1; \
	done; \
	echo "the test database did not become ready" && exit 1

integration-stop:
	-docker rm -f bridge-test-db

lint:
	golangci-lint run ./...

fmt:
	gofmt -l -w .

vet:
	go vet ./...

clean:
	rm -rf bin/

build-binary:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY) ./cmd/$(BINARY)

# Cross-compile for the release platforms.
build-all:
	@mkdir -p bin
	@for arch in amd64 arm64; do \
		echo "Building $(BINARY) linux/$$arch..."; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath \
			-ldflags="$(LDFLAGS)" -o bin/$(BINARY)-linux-$$arch ./cmd/$(BINARY); \
	done

install-tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install helm.sh/helm/v3/cmd/helm@$(HELM_VERSION)
	go install github.com/norwoodj/helm-docs/cmd/helm-docs@$(HELM_DOCS_VERSION)

# gitleaks, downloaded rather than compiled: it is a static binary and the
# release tarball is seconds where `go install` is a minute of dependencies.
bin/gitleaks:
	@mkdir -p bin
	curl -sSfL "https://github.com/gitleaks/gitleaks/releases/download/$(GITLEAKS_VERSION)/gitleaks_$(GITLEAKS_VERSION:v%=%)_linux_x64.tar.gz" \
		| tar xz -C bin gitleaks
	@chmod +x bin/gitleaks

# What ci.yml runs. The working tree, so the result depends only on the commit
# under test and not on how much history the checkout happened to fetch.
secret-scan: bin/gitleaks
	bin/gitleaks dir . --no-banner --redact

# The whole history, for an audit. Deliberately not in CI: a leak in an old
# commit would turn every run red until the history is rewritten, which is a
# decision to take once and not a gate on unrelated work.
secret-scan-history: bin/gitleaks
	bin/gitleaks git . --no-banner --redact

helm-docs:
	helm-docs --chart-search-root charts/

helm-lint: helm-verify

CHART ?= charts/forgejo-github-bridge

# The values a real deployment must supply. The chart refuses to render without
# them, which is deliberate and is asserted below.
HELM_NO_REPOSITORIES = --set database.existingSecret=bridge-db \
                       --set github.appID=123456 \
                       --set github.existingSecret=bridge-github \
                       --set forgejo.url=http://forgejo-http.forgejo:3000 \
                       --set forgejo.existingSecret=bridge-forgejo
HELM_MINIMUM = $(HELM_NO_REPOSITORIES) \
               --set 'repositories[0].github=metapatrol/example' \
               --set 'repositories[0].forgejo=metapatrol/example'

# What ci.yml runs, so that `make helm-lint` and the helm job cannot drift apart.
#
# `helm lint` alone is not a gate: it reports a template that refuses to render
# as an INFO line and still exits 0. `helm template` is what actually fails, so
# every check below goes through it.
helm-verify:
	@set -eu; \
	echo "--- lint"; \
	helm lint $(CHART) $(HELM_MINIMUM); \
	echo "--- render, minimum values"; \
	helm template $(CHART) $(HELM_MINIMUM) > /dev/null; \
	echo "--- render, everything switched on"; \
	helm template $(CHART) $(HELM_MINIMUM) \
		--set serviceMonitor.enabled=true \
		--set networkPolicy.enabled=true \
		--set forgejo.caSecret=forgejo-ca \
		--set forgejo.webURL=https://forgejo.example.com \
		--set 'userMapping.ska=denisneuling' \
		--set forgejo.contribExistingSecret=bridge-contrib \
		--set 'repositories[0].contrib=github-contrib/example' \
		--set 'repositories[0].targetBranches={develop}' \
		--set 'repositories[0].rebaseRequests=true' > /dev/null; \
	echo "--- referenced Secret keys are honoured, not hardcoded"; \
	$(MAKE) --no-print-directory helm-contains \
		WHY="the database key CloudNativePG writes" WANT="key: uri" ARGS="$(HELM_MINIMUM)"; \
	$(MAKE) --no-print-directory helm-contains \
		WHY="a different database key" WANT="key: dsn" \
		ARGS="$(HELM_MINIMUM) --set database.existingSecretKey=dsn"; \
	$(MAKE) --no-print-directory helm-contains \
		WHY="the repository list in the config file" WANT="metapatrol/example" ARGS="$(HELM_MINIMUM)"; \
	$(MAKE) --no-print-directory helm-contains \
		WHY="the App ID as a plain integer" WANT='"123456"' ARGS="$(HELM_MINIMUM)"; \
	$(MAKE) --no-print-directory helm-rejects-pattern \
		WHY="scientific notation in a rendered value" PATTERN="[0-9]e\+[0-9]" \
		ARGS="$(HELM_MINIMUM) --set-json github.appID=12345678"; \
	$(MAKE) --no-print-directory helm-contains \
		WHY="the contrib token from its Secret" WANT="BRIDGE_FORGEJO_CONTRIB_TOKEN" \
		ARGS="$(HELM_MINIMUM) --set forgejo.contribExistingSecret=c --set 'repositories[0].contrib=github-contrib/example' --set 'repositories[0].targetBranches={develop}'"; \
	$(MAKE) --no-print-directory helm-contains \
		WHY="the target branches in the config file" WANT='- "develop"' \
		ARGS="$(HELM_MINIMUM) --set forgejo.contribExistingSecret=c --set 'repositories[0].contrib=github-contrib/example' --set 'repositories[0].targetBranches={develop}'"; \
	echo "--- the chart must refuse what it says it refuses"; \
	$(MAKE) --no-print-directory helm-refuses \
		WHY="pull requests without the contrib token" \
		ARGS="$(HELM_MINIMUM) --set 'repositories[0].contrib=github-contrib/example' --set 'repositories[0].targetBranches={develop}'"; \
	$(MAKE) --no-print-directory helm-refuses \
		WHY="pull requests without target branches" \
		ARGS="$(HELM_MINIMUM) --set forgejo.contribExistingSecret=c --set 'repositories[0].contrib=github-contrib/example'"; \
	$(MAKE) --no-print-directory helm-refuses \
		WHY="no database" ARGS="$(HELM_MINIMUM) --set database.existingSecret="; \
	$(MAKE) --no-print-directory helm-refuses \
		WHY="no GitHub App" ARGS="$(HELM_MINIMUM) --set github.appID="; \
	$(MAKE) --no-print-directory helm-refuses \
		WHY="no GitHub key" ARGS="$(HELM_MINIMUM) --set github.existingSecret="; \
	$(MAKE) --no-print-directory helm-refuses \
		WHY="no Forgejo URL" ARGS="$(HELM_MINIMUM) --set forgejo.url="; \
	$(MAKE) --no-print-directory helm-refuses \
		WHY="no Forgejo token" ARGS="$(HELM_MINIMUM) --set forgejo.existingSecret="; \
	$(MAKE) --no-print-directory helm-refuses \
		WHY="no repositories" ARGS="$(HELM_NO_REPOSITORIES)"; \
	echo "helm chart OK"

# Asserts that a render contains something. The Secret keys in particular:
# rendering successfully proves nothing about which key was referenced, and a
# hardcoded one renders just as cleanly as a configured one.
.PHONY: helm-contains
helm-contains:
	@if helm template $(CHART) $(ARGS) 2>/dev/null | grep -qF -- '$(WANT)'; then \
		echo "    honoured: $(WHY)"; \
	else \
		echo "ERROR: $(WHY) is not in the rendered output; expected $(WANT)"; \
		exit 1; \
	fi

# Asserts that a pattern is absent from the rendered output.
#
# Helm carries YAML numbers as float64, so `quote` on a large one produces
# "1.2345678e+07" — a valid manifest whose value the service cannot parse. A
# GitHub App ID is exactly such a number.
.PHONY: helm-rejects-pattern
helm-rejects-pattern:
	@if helm template $(CHART) $(ARGS) 2>/dev/null | grep -qE '$(PATTERN)'; then \
		echo "ERROR: $(WHY) is in the rendered output"; \
		helm template $(CHART) $(ARGS) 2>/dev/null | grep -E '$(PATTERN)'; \
		exit 1; \
	else \
		echo "    absent as it should be: $(WHY)"; \
	fi

# Asserts that a render fails. The chart's guards are a feature — without this,
# deleting a `fail` from _helpers.tpl passes CI and ships a release that starts
# and then does nothing, or does it against the wrong forge.
.PHONY: helm-refuses
helm-refuses:
	@if helm template $(CHART) $(ARGS) > /dev/null 2>&1; then \
		echo "ERROR: the chart rendered although it should have refused: $(WHY)"; \
		exit 1; \
	else \
		echo "    refused as it should: $(WHY)"; \
	fi

helm-package:
	helm package $(CHART) --destination bin/

changelog:
	git-cliff --config cliff.toml -o CHANGELOG.md
