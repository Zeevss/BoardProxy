.PHONY: all build windows test race vet fmt tidy clean mobile-check mobile-aar

all: fmt vet test build

BIN_DIR := bin
BINARY := $(BIN_DIR)/bproxy
WINDOWS_BINARY := $(BIN_DIR)/bproxy.exe
MOBILE_JAVA_PKG ?= io.boardproxy
MOBILE_TARGETS ?= android/arm64,android/arm,android/amd64
MOBILE_OUTPUT ?= bin/boardproxy.aar
MOBILE_ANDROID_API ?= 26
MOBILE_CGO_LDFLAGS ?= -O2 -g -Wl,-z,max-page-size=16384
MOBILE_GOFLAGS ?= -buildvcs=false

build:
	go build -o $(BINARY) ./cmd/bproxy

windows:
	GOOS=windows GOARCH=amd64 go build -o $(WINDOWS_BINARY) ./cmd/bproxy


test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

mobile-check:
	GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./pkg/mobile
	mkdir -p /tmp/bproxy-gobind
	go tool gobind -lang=java -javapkg $(MOBILE_JAVA_PKG) -outdir /tmp/bproxy-gobind ./pkg/mobile

mobile-aar:
	mkdir -p $(dir $(MOBILE_OUTPUT))
	GOFLAGS="$(MOBILE_GOFLAGS)" CGO_LDFLAGS="$(MOBILE_CGO_LDFLAGS)" gomobile bind -target=$(MOBILE_TARGETS) -androidapi=$(MOBILE_ANDROID_API) -javapkg $(MOBILE_JAVA_PKG) -o $(MOBILE_OUTPUT) ./pkg/mobile

clean:
	rm -f $(BINARY) $(WINDOWS_BINARY) $(MOBILE_OUTPUT)
