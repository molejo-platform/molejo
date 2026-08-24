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
    go tool oapi-codegen -config services/control-plane-api/oapi-codegen.yaml contracts/openapi/control-plane-v1.yaml
    go tool sqlc generate -f services/control-plane-api/sqlc.yaml
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

control-plane-race-test:
    GOCACHE="/tmp/fruto-go-race-cache" GOMODCACHE="/tmp/fruto-go-mod-cache" go test -race ./services/control-plane-api/internal/domain ./services/control-plane-api/internal/api

control-plane-build:
    GOCACHE="/tmp/fruto-go-cache" GOMODCACHE="/tmp/fruto-go-mod-cache" go build -o /tmp/fruto-control-plane-api ./services/control-plane-api/cmd/control-plane-api

control-plane-verify: control-plane-test control-plane-race-test control-plane-build

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

dev-api-http:
    FRUTO_DATABASE_URL="postgres://fruto:fruto@127.0.0.1:55432/fruto?sslmode=disable" \
    FRUTO_MODE=development FRUTO_PUBLIC_URL="http://127.0.0.1:5173" \
    FRUTO_ALLOWED_ORIGIN="http://127.0.0.1:5173" \
    FRUTO_ALLOWED_HOSTS="127.0.0.1:5173,127.0.0.1:8080" \
    FRUTO_ALLOWED_REGISTRIES="ghcr.io" FRUTO_COOKIE_SECURE=false \
    GOCACHE="/tmp/fruto-go-cache" GOMODCACHE="/tmp/fruto-go-mod-cache" \
    go run ./services/control-plane-api/cmd/control-plane-api serve

dev-frontend-http: frontend-install
    VITE_API_PROXY_TARGET="http://127.0.0.1:8080" corepack pnpm --filter @fruto-platform/console-web dev --host 127.0.0.1 --port 5173

dev-tls-cert:
    #!/usr/bin/env bash
    set -euo pipefail
    command -v mkcert >/dev/null 2>&1 || { echo "mkcert is required" >&2; exit 2; }
    mkdir -p .local/certs
    mkcert -install
    mkcert -cert-file .local/certs/console.localhost.pem -key-file .local/certs/console.localhost-key.pem console.localhost localhost 127.0.0.1 ::1

dev-api-tls:
    FRUTO_DATABASE_URL="postgres://fruto:fruto@127.0.0.1:55432/fruto?sslmode=disable" \
    FRUTO_MODE=development FRUTO_PUBLIC_URL="https://console.localhost:5173" \
    FRUTO_ALLOWED_ORIGIN="https://console.localhost:5173" \
    FRUTO_ALLOWED_HOSTS="console.localhost:5173,127.0.0.1:8080" \
    FRUTO_ALLOWED_REGISTRIES="ghcr.io" FRUTO_COOKIE_SECURE=true \
    GOCACHE="/tmp/fruto-go-cache" GOMODCACHE="/tmp/fruto-go-mod-cache" \
    go run ./services/control-plane-api/cmd/control-plane-api serve

dev-frontend-tls: frontend-install
    test -s .local/certs/console.localhost.pem
    test -s .local/certs/console.localhost-key.pem
    VITE_API_PROXY_TARGET="http://127.0.0.1:8080" \
    VITE_TLS_CERT="$(pwd)/.local/certs/console.localhost.pem" \
    VITE_TLS_KEY="$(pwd)/.local/certs/console.localhost-key.pem" \
    corepack pnpm --filter @fruto-platform/console-web dev --host console.localhost --port 5173

control-plane-e2e-kind:
    bash test/e2e/control-plane-kind.sh

e2e:
    bash test/e2e/run.sh

e2e-public:
    E2E_PUBLIC_EGRESS_URL="${E2E_PUBLIC_EGRESS_URL:-https://api.github.com/zen}" bash test/e2e/run.sh

e2e-frontend-k3s:
    bash test/e2e/k3s-frontend.sh

control-plane-preflight-k3s:
    bash test/e2e/control-plane-k3s-preflight.sh

control-plane-render-release:
    bash test/e2e/render-control-plane-release.sh

control-plane-build-release:
    bash test/e2e/build-control-plane-release.sh

control-plane-prepare-k3s:
    bash test/e2e/prepare-control-plane-k3s.sh

control-plane-apply-k3s:
    bash test/e2e/apply-control-plane-k3s.sh

control-plane-accept-k3s:
    bash test/e2e/accept-control-plane-k3s.sh

verify: generate fmt-check lint test control-plane-verify frontend-check frontend-test

ci:
    bash test/generated/check.sh
    just verify
    just control-plane-integration-test
    just control-plane-e2e-kind
    just e2e
