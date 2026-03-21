BINARY := spark-ui-assist
GO     := go
GOFMT  := gofmt

.PHONY: all fmt-check vet test build

all: build

fmt-check:
	@unformatted=$$($(GOFMT) -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt-formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet: fmt-check
	$(GO) vet ./...

test: vet
	$(GO) test -race ./...

build: fmt-check
	$(GO) build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/spark-ui-assist
