.PHONY: default
default: build

VERSION := 0.4.0
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

.PHONY: release
release: dist
	if ! git tag -l | fgrep v$(VERSION); then (git tag v$(VERSION)); fi
	hub release create -a ktail-$(VERSION)-$(ARCH).tar.gz -m "Released $(VERSION)." v$(VERSION)
