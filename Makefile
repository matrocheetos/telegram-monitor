BINARY_NAME=telegram-monitor
BUILD_DIR=bin
GO=go

ifeq ($(OS),Windows_NT)
	RM_CMD=if exist $(BUILD_DIR) rmdir /s /q $(BUILD_DIR)
else
	RM_CMD=rm -rf $(BUILD_DIR)
endif

.PHONY: all build test test-race bench lint vulncheck docker-build clean fmt

all: lint test build

build:
	$(GO) build -v -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/telegram-monitor

test:
	$(GO) test -v ./...

test-race:
	$(GO) test -v -race ./...

bench:
	$(GO) test -v -bench=. -benchmem ./...

lint:
	golangci-lint run ./...

vulncheck:
	govulncheck ./...

docker-build:
	docker build -t $(BINARY_NAME):latest .

clean:
	@$(GO) clean
	@$(RM_CMD)

fmt:
	$(GO) fmt ./...

