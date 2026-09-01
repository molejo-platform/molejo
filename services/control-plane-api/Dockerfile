FROM golang:1.26.7-alpine@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY packages packages
COPY contracts contracts
COPY services/control-plane-api services/control-plane-api

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=devel
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -buildvcs=false -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/control-plane-api ./services/control-plane-api/cmd/control-plane-api

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/control-plane-api /control-plane-api
USER 65532:65532
ENTRYPOINT ["/control-plane-api"]
