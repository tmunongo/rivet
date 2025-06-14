.PHONY: add-git format lint

BINARY := rivet
VERSION=0.0.1
BUILD=`date +%FT%T%z`

# Setup the -ldflags option for go build here, interpolate the variable values
LDFLAGS_f1=-ldflags "-w -s -X main.version=${VERSION} -X main.build=${BUILD}"

# Builds the project
build:
	go build ${LDFLAGS_f1} -o ${BINARY}

# Installs our project: copies binaries
install:
	go install ${LDFLAGS_f1}

# Go source files
GO_SRCS := $(shell find . -name '*.go' -print0 | xargs -0)

format:
	@echo "Running go fmt..."
	@go fmt $(GO_SRCS)

lint:
	@echo "Running go lint..."
	@go install golang.org/x/lint/golint@latest # Install golint if not already installed
	@golint ./...

add-git: format lint
	@echo "Adding formatted and linted files to git..."
	@git add .
	@echo "Files added to git. Remember to commit them!"

all: add-git
