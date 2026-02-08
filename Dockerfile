FROM golang:1.22-bookworm AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/capture-controller ./cmd/capture-controller

FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends bash tcpdump ca-certificates && rm -rf /var/lib/apt/lists/*
COPY scripts/tcpdump /usr/local/bin/tcpdump
RUN chmod +x /usr/local/bin/tcpdump
COPY --from=builder /out/capture-controller /usr/local/bin/capture-controller
ENTRYPOINT ["/usr/local/bin/capture-controller"]
