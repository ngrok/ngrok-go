# Wrapper to define common variables

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

ifndef ignore-not-found
  ignore-not-found = false
endif

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
BUF ?= $(LOCALBIN)/buf-$(BUF_VERSION)


## Tool Versions
BUF_VERSION ?= v1.52.1


# ==============================================
# Includes:
# ==============================================
include tools/make/help.mk
include tools/make/tools.mk
include tools/make/generate.mk
