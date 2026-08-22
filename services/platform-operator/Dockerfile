# syntax=docker/dockerfile:1

FROM golang:1.26.7-alpine@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY packages/ packages/
COPY services/platform-operator/ services/platform-operator/

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=devel
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -buildvcs=false -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/platform-operator ./services/platform-operator/cmd/manager

FROM scratch

USER 65532:65532
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/platform-operator /platform-operator

ENTRYPOINT ["/platform-operator"]
