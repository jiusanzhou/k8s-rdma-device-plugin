FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-s -w -X github.com/jiusanzhou/k8s-rdma-device-plugin/cmd/k8s-rdma-device-plugin/app.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /k8s-rdma-device-plugin \
    ./cmd/k8s-rdma-device-plugin

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /k8s-rdma-device-plugin /usr/local/bin/k8s-rdma-device-plugin
ENTRYPOINT ["k8s-rdma-device-plugin"]
