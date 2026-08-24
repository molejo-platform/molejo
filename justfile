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
    corepack pnpm --filter @fruto-platform/console-web generate:api-types

test:
    KUBEBUILDER_ASSETS="$(go tool setup-envtest use -p path {{ envtest_version }})" go test ./...

frontend-node-check:
    test "$(node --version)" = "v$(cat .node-version)"

frontend-install: frontend-node-check
    CI=true corepack pnpm install --frozen-lockfile --ignore-scripts

frontend-check: frontend-install
    corepack pnpm frontend:check

frontend-build: frontend-install
    corepack pnpm frontend:build

frontend-test: frontend-install
    corepack pnpm frontend:test
    bash test/frontend/run.sh

audit-frontend-images:
    bash test/security/base-images.sh

control-plane-test:
    GOCACHE="/tmp/fruto-go-cache" GOMODCACHE="/tmp/fruto-go-mod-cache" go test ./services/control-plane-api/...

control-plane-build:
    GOCACHE="/tmp/fruto-go-cache" GOMODCACHE="/tmp/fruto-go-mod-cache" go build -o /tmp/fruto-control-plane-api ./services/control-plane-api/cmd/control-plane-api

control-plane-verify: control-plane-test control-plane-build

control-plane-integration-test:
    #!/usr/bin/env bash
    set -euo pipefail
    compose_project="fruto-control-plane-integration-$$"
    compose_file="deploy/control-plane/docker-compose.yaml"
    postgres_port="$(node -e 'const net=require("net");const server=net.createServer();server.listen(0,"127.0.0.1",()=>{console.log(server.address().port);server.close();});')"
    cleanup() {
      exit_code=$?
      if ! FRUTO_POSTGRES_PORT="$postgres_port" docker compose --project-name "$compose_project" -f "$compose_file" down >/dev/null 2>&1 && [[ "$exit_code" -eq 0 ]]; then
        exit_code=1
      fi
      trap - EXIT
      exit "$exit_code"
    }
    trap cleanup EXIT
    FRUTO_POSTGRES_PORT="$postgres_port" docker compose --project-name "$compose_project" -f "$compose_file" up -d postgres
    ready=false
    for _ in $(seq 1 60); do
      if FRUTO_POSTGRES_PORT="$postgres_port" docker compose --project-name "$compose_project" -f "$compose_file" exec -T postgres pg_isready -U fruto -d fruto >/dev/null 2>&1; then
        ready=true
        break
      fi
      sleep 1
    done
    if [[ "$ready" != true ]]; then
      FRUTO_POSTGRES_PORT="$postgres_port" docker compose --project-name "$compose_project" -f "$compose_file" logs postgres >&2
      exit 1
    fi
    FRUTO_TEST_DATABASE_URL="postgres://fruto:fruto@127.0.0.1:${postgres_port}/fruto?sslmode=disable" \
      GOCACHE="/tmp/fruto-go-cache" GOMODCACHE="/tmp/fruto-go-mod-cache" \
      go test -count=1 -p=1 ./services/control-plane-api/internal/store ./services/control-plane-api/internal/api

db-up:
    docker compose -f deploy/control-plane/docker-compose.yaml up -d postgres

db-migrate:
    FRUTO_DATABASE_URL="postgres://fruto:fruto@127.0.0.1:55432/fruto?sslmode=disable" GOCACHE="/tmp/fruto-go-cache" GOMODCACHE="/tmp/fruto-go-mod-cache" go run ./services/control-plane-api/cmd/control-plane-api migrate

db-down:
    docker compose -f deploy/control-plane/docker-compose.yaml down

control-plane-e2e-kind:
    bash test/e2e/control-plane-kind.sh

e2e:
    bash test/e2e/run.sh

e2e-public:
    E2E_PUBLIC_EGRESS_URL="${E2E_PUBLIC_EGRESS_URL:-https://api.github.com/zen}" bash test/e2e/run.sh

e2e-frontend-k3s:
    bash test/e2e/k3s-frontend.sh

verify: generate fmt-check lint test control-plane-verify frontend-check frontend-test

ci:
    bash test/generated/check.sh
    just verify
    just control-plane-integration-test
    just control-plane-e2e-kind
    just e2e
