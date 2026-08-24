FROM golang:1.27.0@sha256:65b6f280bf050ec5af12716857e8ea8439d694dbba8f31ceeb7630670071f2bb AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY packages packages
COPY services/control-plane-api services/control-plane-api
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/control-plane-api ./services/control-plane-api/cmd/control-plane-api

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/control-plane-api /control-plane-api
USER 65532:65532
ENTRYPOINT ["/control-plane-api"]
