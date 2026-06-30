# Colors
NOCOLOR=\033[0m
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[0;33m
FLAGVERSION=-ldflags "-s -w -X github.com/Kentalives/LifeRouter/cmd.BuildTime=`date '+%Y-%m-%dT%H:%M:%SZ'` -X github.com/Kentalives/LifeRouter/cmd.GitInfo=`git describe --tags --long --always`"

# Docker variables
NAME := "github.com/Kentalives/LifeRouter-amd64"
IMAGEVERSION := $(shell git describe --tags --always)
IMAGENAME := github.com/Kentalives/LifeRouter/${NAME}:${IMAGEVERSION}

# Optional Go build-tag override retained for compatibility.
export MODULES
COVERPKG ?= ./...

ifndef MODULES
TAGS=-tags all
else
TAGS=-tags "$(MODULES)"
endif

all:
	@echo ''
	@echo '   How to build:'
	@echo '       make build                                - Build the service'
	@echo ''

deps:
	@echo "${YELLOW}== Generating golang vendor directory...${NOCOLOR}"
	go mod tidy && go mod vendor
	@echo "${YELLOW}== Generating golang vendor directory...${NOCOLOR} ${GREEN} [OK] ${NOCOLOR}"

clean:
	@echo "${YELLOW}== Cleaning...${NOCOLOR}"
	@echo "${YELLOW}== Cleaning...${NOCOLOR} ${GREEN} [OK] ${NOCOLOR}"

build:
	@echo "${YELLOW}== Building ...${NOCOLOR}"
	go build ${FLAGVERSION} ${TAGS} -v
	@echo "${YELLOW}== Building ...${NOCOLOR} ${GREEN} [OK] ${NOCOLOR}"

tests: test

.PHONY: test
test:
	@echo "${YELLOW}== Running tests...${NOCOLOR}"
	go test ${TAGS} -v -cover "-coverprofile=coverage.out" ./...
	@echo "${YELLOW}== Running tests...${NOCOLOR} ${GREEN} [Done] ${NOCOLOR}"
	@echo ""
	@echo "Run this in order to see an html report: ${GREEN}go tool cover -html=coverage.out${NOCOLOR}"

.PHONY: test-cover-full
test-cover-full:
	@echo "${YELLOW}== Running tests with full cross-package coverage...${NOCOLOR}"
	go test ${TAGS} -p 1 -v "-coverpkg=${COVERPKG}" "-coverprofile=coverage-full.out" ./...
	@echo "${YELLOW}== Running tests with full cross-package coverage...${NOCOLOR} ${GREEN} [Done] ${NOCOLOR}"
	@echo ""
	@echo "Run this in order to see an html report: ${GREEN}go tool cover -html=coverage-full.out${NOCOLOR}"

.PHONY: cover-html
cover-html:
	go tool cover "-html=coverage.out"

.PHONY: cover-full-html
cover-full-html:
	go tool cover "-html=coverage-full.out"

.PHONY: cover-func
cover-func:
	go tool cover "-func=coverage.out"

.PHONY: cover-full-func
cover-full-func:
	go tool cover "-func=coverage-full.out"

# ===================================================
# Image for production environment
# ===================================================
.PHONY: docker-image
docker-image:
	@echo "YELLOW== Building docker: IMAGENAMENOCOLOR"
	docker build --rm -t IMAGENAME .
	docker image prune -f --filter="dangling=true"
	@echo "YELLOW== Building docker: IMAGENAMENOCOLOR GREEN [OK] NOCOLOR"
