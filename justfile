set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

envtest_version := "1.36.2"

default: verify

fmt:
    go fmt ./...

fmt-check:
    test -z "$(find packages services -type f -name '*.go' -exec gofmt -l {} +)"

lint:
    go vet ./...

generate:
    go tool controller-gen object paths=./packages/kubernetes-api/apis/...
    go tool controller-gen crd paths=./packages/kubernetes-api/apis/... output:crd:artifacts:config=deploy/crds
    go tool controller-gen rbac:roleName=platform-operator paths=./services/platform-operator/... output:rbac:artifacts:config=deploy/operator/rbac

test:
    KUBEBUILDER_ASSETS="$(go tool setup-envtest use -p path {{envtest_version}})" go test ./...

e2e:
    bash test/e2e/run.sh

verify: generate fmt-check lint test
