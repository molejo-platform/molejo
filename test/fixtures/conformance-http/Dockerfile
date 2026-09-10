FROM golang:1.26.7-alpine@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468 AS build

WORKDIR /src
COPY test/fixtures/conformance-http/main.go ./main.go
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/conformance-http ./main.go

FROM scratch
USER 65532:65532
COPY --from=build /out/conformance-http /conformance-http
EXPOSE 8080
ENTRYPOINT ["/conformance-http"]
