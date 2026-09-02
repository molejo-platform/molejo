FROM golang:1.26.7-alpine@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY contracts contracts
COPY packages packages
COPY services/cluster-agent services/cluster-agent

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=devel
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -buildvcs=false -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/cluster-agent ./services/cluster-agent/cmd/cluster-agent

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/cluster-agent /cluster-agent
USER 65532:65532
ENTRYPOINT ["/cluster-agent"]
