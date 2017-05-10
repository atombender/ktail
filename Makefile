.PHONY: default
default: build

VERSION := 0.1.0
NAME := ktail
ARCH := $(shell uname -m)

BUILD_DIR := $(PWD)/build
GOPATH := $(BUILD_DIR)/go
GO_PACKAGE_PATH := $(GOPATH)/src/github.com/atombender/ktail
GO := env GOPATH=$(GOPATH) go

GO_SRC := $(shell find . -name '*.go' -type f | fgrep -v ./vendor/ | fgrep -v '${BUILD_DIR}')

.PHONY: build
build: $(BUILD_DIR)/ktail

$(BUILD_DIR)/ktail: $(GO_SRC)
	mkdir -p $(GOPATH)/src/github.com/atombender
	ln -sf $(PWD) $(GOPATH)/src/github.com/atombender/ktail
	$(GO) build -o ${BUILD_DIR}/ktail github.com/atombender/ktail

.PHONY: dist
dist: build
	mkdir -p dist
	cp LICENSE README.md build/ktail dist/
	tar cvfz $(NAME)-$(VERSION)-$(ARCH).tar.gz -C dist .
