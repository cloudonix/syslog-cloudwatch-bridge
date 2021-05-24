.PHONY: build linux clean deps

all: deps build

build:
	go build

linux: *.go
	CGO_ENABLED=0 GOOS=linux go build -a -tags netgo -ldflags '-w' .

clean:
	rm -f syslog-cloudwatch-bridge

docker-build: clean linux
	$(BUILD_CMD) -t ${IMG} -f Dockerfile .

deps:
	go get -d -v ./...
