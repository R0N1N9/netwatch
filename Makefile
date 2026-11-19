BINARY := netwatch
BINDIR := bin
PKG    := .

.PHONY: build gui demo run sweep test vet tidy clean caps

build:
	CGO_ENABLED=0 go build -o $(BINDIR)/$(BINARY) $(PKG)

## Grant capture/injection (and nftables) capabilities without running as root.
caps: build
	sudo setcap cap_net_raw,cap_net_admin+eip $(BINDIR)/$(BINARY)

## Launch the Cyberpunk web console (live).
gui: build
	$(BINDIR)/$(BINARY) gui

## Preview the console with sample data (no privileges needed).
demo: build
	$(BINDIR)/$(BINARY) gui --demo

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
