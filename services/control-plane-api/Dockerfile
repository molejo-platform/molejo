FROM golang:1.27.0 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY packages packages
COPY services/control-plane-api services/control-plane-api
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/control-plane-api ./services/control-plane-api/cmd/control-plane-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/control-plane-api /control-plane-api
USER nonroot:nonroot
ENTRYPOINT ["/control-plane-api"]
