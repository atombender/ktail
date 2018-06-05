.PHONY: default
default: build

NAME := ktail
ARCH := $(shell uname -m)

BUILD_DIR := $(PWD)/build
GOPATH := $(BUILD_DIR)/go
GO_PACKAGE_PATH := $(GOPATH)/src/github.com/atombender/ktail
GO := env GOPATH=$(GOPATH) go

GO_SRC := $(shell find . -name '*.go' -type f | fgrep -v ./vendor/ | fgrep -v '${BUILD_DIR}')

.PHONY: build
build: $(BUILD_DIR)/ktail

$(BUILD_DIR):
	mkdir $(BUILD_DIR)

$(BUILD_DIR)/ktail: $(BUILD_DIR) $(GO_SRC)
	mkdir -p $(GOPATH)/src/github.com/atombender
	ln -sf $(PWD) $(GOPATH)/src/github.com/atombender/ktail
	$(GO) build -i -o ${BUILD_DIR}/ktail github.com/atombender/ktail

.PHONY: clean
clean: $(BUILD_DIR)
	rm -f $(BUILD_DIR)/ktail
