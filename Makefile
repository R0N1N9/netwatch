BINARY := netrunner
BINDIR := bin
PKG    := .

.PHONY: build run sweep test vet tidy clean caps

build:
	CGO_ENABLED=0 go build -o $(BINDIR)/$(BINARY) $(PKG)

## Grant capture/injection (and nftables) capabilities without running as root.
caps: build
	sudo setcap cap_net_raw,cap_net_admin+eip $(BINDIR)/$(BINARY)

run: build
	$(BINDIR)/$(BINARY) watch

sweep: build
	$(BINDIR)/$(BINARY) sweep

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf $(BINDIR)
