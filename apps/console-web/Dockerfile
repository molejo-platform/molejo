FROM golang:1.26.7-alpine@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps/console-web apps/console-web

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=devel
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -buildvcs=false -trimpath \
    -ldflags="-s -w" \
    -o /out/console-web ./apps/console-web

FROM scratch

USER 65532:65532
COPY --from=build /out/console-web /console-web
EXPOSE 8080
ENTRYPOINT ["/console-web"]
