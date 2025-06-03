.PHONY: add-git format lint

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