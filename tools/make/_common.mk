# Wrapper to define common variables

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

ifndef ignore-not-found
  ignore-not-found = false
endif

## Location to install dependencies to
TOOLS_BIN_DIR ?= $(shell pwd)/tools/bin
$(TOOLS_BIN_DIR):
	mkdir -p $(TOOLS_BIN_DIR)

## Tool Versions
BUF_VERSION ?= v1.52.1

## Tool Binaries
BUF ?= $(TOOLS_BIN_DIR)/buf-$(BUF_VERSION)
# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/${TOOLS_BIN_DIR}
else
GOBIN=$(shell go env GOBIN)
endif


# ==============================================
# Includes:
# ==============================================
include tools/make/help.mk
include tools/make/tools.mk
include tools/make/generate.mk
