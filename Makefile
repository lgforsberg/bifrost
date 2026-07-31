BINARY := bifrost
PKG    := ./cmd/bifrost
BINDIR := bin

.PHONY: all build install test fmt vet tidy clean

all: build

build:
	@mkdir -p $(BINDIR)
	go build -o $(BINDIR)/$(BINARY) $(PKG)

install:
	go install $(PKG)

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf $(BINDIR)
