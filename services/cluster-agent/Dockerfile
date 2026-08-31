FROM golang:1.27.0@sha256:65b6f280bf050ec5af12716857e8ea8439d694dbba8f31ceeb7630670071f2bb AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY contracts contracts
COPY services/cluster-agent services/cluster-agent
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/cluster-agent ./services/cluster-agent/cmd/cluster-agent

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/cluster-agent /cluster-agent
USER 65532:65532
ENTRYPOINT ["/cluster-agent"]
