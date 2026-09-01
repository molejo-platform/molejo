set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

envtest_version := "1.36.2"

default: verify

fmt:
    go fmt ./...

fmt-check:
    test -z "$(find packages services test -type f -name '*.go' -exec gofmt -l {} + 2>/dev/null)"

lint:
    go vet ./...

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

test: operator-test agent-test contract-test

verify: generate fmt-check lint test control-plane-build
