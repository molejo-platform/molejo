set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

envtest_version := "1.36.2"
tool_mod := "tools/go.mod"
go_sources := "apps contracts packages services test tools/cmd tools/internal"

default: verify

fmt:
    go tool -modfile={{ tool_mod }} gofumpt -w {{ go_sources }}
    go tool -modfile={{ tool_mod }} gci write --skip-generated -s standard -s default -s localmodule {{ go_sources }}

fmt-check:
    test -z "$(find {{ go_sources }} -type f -name '*.go' -exec gofmt -l {} + 2>/dev/null)"
    test -z "$(go tool -modfile={{ tool_mod }} gofumpt -l {{ go_sources }})"
    test -z "$(go tool -modfile={{ tool_mod }} gci list --skip-generated -s standard -s default -s localmodule {{ go_sources }})"

lint:
    GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C tools build -o /tmp/molejo-golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint
    GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" GOLANGCI_LINT_CACHE="${GOLANGCI_LINT_CACHE:-/tmp/molejo-golangci-cache}" /tmp/molejo-golangci-lint run ./...

mod-check:
    go mod tidy -diff
    go mod verify
    go -C tools mod tidy -diff
    go -C tools mod verify

hooks-install:
    hook_bin="$(git rev-parse --path-format=absolute --git-path lefthook)"; GOCACHE="${GOCACHE:-/tmp/molejo-go-cache}" go -C tools build -o "$hook_bin" github.com/evilmartians/lefthook/v2; "$hook_bin" install

generate:
    BUF_CACHE_DIR="${BUF_CACHE_DIR:-/tmp/molejo-buf-cache}" go run github.com/bufbuild/buf/cmd/buf@v1.72.0 lint
    BUF_CACHE_DIR="${BUF_CACHE_DIR:-/tmp/molejo-buf-cache}" go run github.com/bufbuild/buf/cmd/buf@v1.72.0 generate
    go tool controller-gen object paths=./packages/kubernetes-api/apis/...
    go tool controller-gen crd paths=./packages/kubernetes-api/apis/... output:crd:artifacts:config=deploy/crds
    go tool controller-gen rbac:roleName=platform-operator paths=./services/platform-operator/... output:rbac:artifacts:config=deploy/operator/rbac
    go tool oapi-codegen -config services/control-plane-api/oapi-codegen.yaml contracts/openapi/control-plane-v1.yaml
    go tool sqlc generate -f services/control-plane-api/sqlc.yaml

operator-test:
    KUBEBUILDER_ASSETS="$(go tool setup-envtest use -p path {{ envtest_version }})" go test ./packages/kubernetes-api/... ./services/platform-operator/...

agent-test:
    go test ./contracts/... ./services/cluster-agent/... ./services/control-plane-api/...

contract-test:
    go test ./test/contracts

control-plane-build:
    go build -o /tmp/molejo-control-plane-api ./services/control-plane-api/cmd/control-plane-api

distribution-build:
    go build -o /tmp/molejoctl ./apps/molejoctl
    go build -o /tmp/molejo-console-web ./apps/console-web
    go -C tools build -o /tmp/molejo-release ./cmd/release

distribution-test:
    go test ./apps/...
    go -C tools test ./cmd/release/... ./internal/release/...

test: operator-test agent-test contract-test distribution-test

integration-test:
    MOLEJO_TESTCONTAINERS=1 go test -count=1 ./services/control-plane-api/internal/api ./services/control-plane-api/internal/store

verify: mod-check generate fmt-check lint test integration-test control-plane-build distribution-build

ci: verify
    git diff --check
    git diff --exit-code
