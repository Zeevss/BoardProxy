.PHONY: build test vet fmt tidy clean deploy all mobile-check mobile-aar

all: fmt vet test build

deploy: build deploy-server

build: client-cli client-windows-cli

BIN_DIR := bin
MOBILE_JAVA_PKG ?= io.boardproxy
MOBILE_TARGETS ?= android/arm64,android/arm,android/amd64
MOBILE_OUTPUT ?= bin/boardproxy.aar
MOBILE_ANDROID_API ?= 26
MOBILE_CGO_LDFLAGS ?= -O2 -g -Wl,-z,max-page-size=16384
MOBILE_GOFLAGS ?= -buildvcs=false

client-cli:
	go build -o $(BIN_DIR)/bproxy-cli ./cmd/bproxy

client-windows-cli:
	GOOS=windows GOARCH=amd64 go build -o $(BIN_DIR)/bproxy-cli.exe ./cmd/bproxy


test:
	go test ./...

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
	rm -rf bin

SERVER = michael

pushing-server:
	@echo "Pushing server..."
	ssh ${SERVER} "pkill bproxy || true"
	tar -C . -cf - . | ssh ${SERVER} "mkdir -p ~/bproxy && tar -xf - -C ~/bproxy"

deploy-server:
	@echo "Deploying the application..."
	ssh ${SERVER} "pkill bproxy || true"
	scp ./bin/bproxy ${SERVER}:~/bproxy-bin
	ssh ${SERVER} "chmod +x ~/bproxy-bin && ~/bproxy-bin serve -b 085db13e3c6d5928cf4ea67cf07fbefc --log debug"
