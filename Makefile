GIT_HEAD_COMMIT ?= $(shell git rev-parse --short HEAD)
VERSION         ?= $(or $(shell git describe --abbrev=0 --tags --match "v*" 2>/dev/null),$(GIT_HEAD_COMMIT))

DEBIAN_INIT_SUFFIX=-debian-init
REDHAT_INIT_SUFFIX=-redhat-init

REGISTRY             ?= ghcr.io
GITHUB_REPOSITORY    ?= weisshorn-cyd/cain
CAIN_IMG             ?= $(REGISTRY)/$(GITHUB_REPOSITORY)
CAIN_DEBIAN_INIT_IMG ?= $(CAIN_IMG)$(DEBIAN_INIT_SUFFIX)
CAIN_REDHAT_INIT_IMG ?= $(CAIN_IMG)$(REDHAT_INIT_SUFFIX)

CAIN_TAGS ?= "latest"


ifdef VERSION
CAIN_TAGS := $(CAIN_TAGS),$(VERSION)
endif

GO=go
KO=ko
BUILDAH=buildah


GOTOOL=$(GO) tool -modfile=$(TOOLSMOD)
GOBUILD=$(GO) build
GOCLEAN=$(GO) clean
GOTIDY=$(GO) mod tidy
GOTEST=$(GO) test

ifdef CI
GIT_USER="github-actions"
else
GIT_USER=$(shell git config --get user.name)
endif

.PHONY: all clean deps verify fmt lint vulncheck build test publish

all: clean deps verify vulncheck fmt lint build

clean:
	$(GOTIDY)
	$(GOCLEAN)

deps:
	$(GO) mod download

verify:
	$(GO) mod verify

fmt: gofumpt
	$(GOTOOL) $(GOFUMPT) -l -w .

lint: golangci-lint clean
	$(GOLANGCI_LINT) run -c .golangci.yaml --fix ./...

vulncheck: govulncheck
	$(GOTOOL) govulncheck -test ./...


KOCACHE ?= /tmp/ko-cache

build: ko
	KOCACHE=$(KOCACHE) KO_DOCKER_REPO=$(CAIN_IMG) \
	$(GOTOOL) ko build ./ --bare --tags $(CAIN_TAGS) --push=false --local

publish: ko
	KOCACHE=$(KOCACHE) KO_DOCKER_REPO=$(CAIN_IMG) \
	$(GOTOOL) ko build ./ --bare --tags $(CAIN_TAGS)

ifdef VERSION

build-init: build-debian-init build-redhat-init
all-init: publish-debian-init publish-redhat-init

.PHONY: build-debian-init publish-debian-init build-redhat-init publish-redhat-init build-init all-init

build-debian-init:
	cd init-container && \
		$(BUILDAH) build -f Containerfile.debian \
		-t $(CAIN_DEBIAN_INIT_IMG):$(VERSION) \
		.

publish-debian-init: build-debian-init
	$(BUILDAH) push $(CAIN_DEBIAN_INIT_IMG):$(VERSION)

build-redhat-init:
	cd init-container && \
		$(BUILDAH) build -f Containerfile.redhat \
		-t $(CAIN_REDHAT_INIT_IMG):$(VERSION) \
		.

publish-redhat-init: build-redhat-init
	$(BUILDAH) push $(CAIN_REDHAT_INIT_IMG):$(VERSION)
endif

test:
	$(GOTEST) -race ./...

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/.bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

TOOLSMOD ?= $(shell pwd)/tools.mod
$(TOOLSMOD):
	$(GO) mod init -modfile $(TOOLSMOD) github.com/$(GITHUB_REPOSITORY)/tools

####################
# -- Tools
####################

.PHONY: tools ko gofumpt govulncheck golangci-lint
tools: ko gofumpt govulncheck

KO           := ko
KO_VERSION   := v0.17.1
KO_LOOKUP    := github.com/google/ko
ko: $(TOOLSMOD)
	@$(GOTOOL) | grep $(KO_LOOKUP) && $(GOTOOL) $(KO) version | grep -q $(KO_VERSION) || \
	$(call go-install-tool,$(KO),$(KO_LOOKUP)@$(KO_VERSION))

GOFUMPT           := gofumpt
GOFUMPT_VERSION   := v0.7.0
GOFUMPT_LOOKUP    := mvdan.cc/gofumpt
gofumpt: $(TOOLSMOD)
	@$(GOTOOL) | grep $(GOFUMPT) && $(GOTOOL) $(GOFUMPT) -version | grep -q $(GOFUMPT_VERSION) || \
	$(call go-install-tool,$(GOFUMPT),$(GOFUMPT_LOOKUP)@$(GOFUMPT_VERSION))

GOVULNCHECK           := govulncheck
GOVULNCHECK_VERSION   := v1.1.4
GOVULNCHECK_LOOKUP    := golang.org/x/vuln/cmd/govulncheck
govulncheck: $(TOOLSMOD)
	@$(GOTOOL) | grep $(GOVULNCHECK) && $(GOTOOL) $(GOVULNCHECK) -version | grep -q $(GOVULNCHECK_VERSION) || \
	$(call go-install-tool,$(GOVULNCHECK),$(GOVULNCHECK_LOOKUP)@$(GOVULNCHECK_VERSION))

GOLANGCI_LINT          := $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION  := 2.0.2
GOLANGCI_LINT_LOOKUP   := golangci/golangci-lint
golangci-lint: ## Download golangci-lint locally if necessary.
	@test -s $(GOLANGCI_LINT) && $(GOLANGCI_LINT) version | grep -q $(GOLANGCI_LINT_VERSION) || \
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(LOCALBIN) v$(GOLANGCI_LINT_VERSION)

define go-install-tool
$(GOTOOL) $(1) -h || { \
    set -e ;\
    GOBIN=$(LOCALBIN) $(GO) get -modfile=$(TOOLSMOD) -tool $(2) ;\
}
endef