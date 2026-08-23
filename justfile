set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

envtest_version := "1.36.2"

default: verify

fmt:
    go fmt ./...

fmt-check:
    test -z "$(find packages services test -type f -name '*.go' -exec gofmt -l {} +)"

lint:
    go vet ./...

generate:
    go tool controller-gen object paths=./packages/kubernetes-api/apis/...
    go tool controller-gen crd paths=./packages/kubernetes-api/apis/... output:crd:artifacts:config=deploy/crds
    go tool controller-gen rbac:roleName=platform-operator paths=./services/platform-operator/... output:rbac:artifacts:config=deploy/operator/rbac

test:
    KUBEBUILDER_ASSETS="$(go tool setup-envtest use -p path {{envtest_version}})" go test ./...

frontend-node-check:
    test "$(node --version)" = "v$(cat .node-version)"

frontend-install: frontend-node-check
    corepack pnpm install --frozen-lockfile

frontend-check: frontend-install
    corepack pnpm frontend:check

frontend-build: frontend-install
    corepack pnpm frontend:build

frontend-test:
    bash test/frontend/run.sh

audit-frontend-images:
    bash test/security/base-images.sh

e2e:
    bash test/e2e/run.sh

e2e-public:
    E2E_PUBLIC_EGRESS_URL="${E2E_PUBLIC_EGRESS_URL:-https://api.github.com/zen}" bash test/e2e/run.sh

e2e-frontend-k3s:
    bash test/e2e/k3s-frontend.sh

verify: generate fmt-check lint test frontend-check frontend-test

ci:
    bash test/generated/check.sh
    just verify
    just e2e
