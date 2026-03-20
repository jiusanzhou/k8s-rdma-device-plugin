BINARY = bin/k8s-rdma-device-plugin
MODULE = github.com/jiusanzhou/k8s-rdma-device-plugin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
IMAGE   ?= k8s-rdma-device-plugin
TAG     ?= $(VERSION)

LDFLAGS = -s -w \
	-X $(MODULE)/cmd/k8s-rdma-device-plugin/app.version=$(VERSION)

.PHONY: all build clean test lint docker

all: build

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-ldflags "$(LDFLAGS)" \
		-o $(BINARY) \
		./cmd/k8s-rdma-device-plugin

build-local:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/k8s-rdma-device-plugin

clean:
	rm -rf bin/

test:
	go test -v -race ./...

lint:
	golangci-lint run ./...

docker:
	docker build -t $(IMAGE):$(TAG) .

docker-push: docker
	docker push $(IMAGE):$(TAG)

fmt:
	gofmt -s -w .

vet:
	go vet ./...

mod:
	go mod tidy
	go mod verify
