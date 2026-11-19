BINARY := netwatch
BINDIR := bin
PKG    := .

.PHONY: build gui demo run sweep test vet tidy clean caps update stop launcher

build:
	CGO_ENABLED=0 go build -o $(BINDIR)/$(BINARY) $(PKG)

## Grant capture/injection (and nftables) capabilities without running as root.
caps: build
	sudo setcap cap_net_raw,cap_net_admin+eip $(BINDIR)/$(BINARY)

## Stop the running copy, pull, rebuild, restore capture rights and start again.
update:
	./update.sh

## Stop a running NetWatch.
stop:
	-pkill -x $(BINARY)

## Put NetWatch in the desktop's application menu (it opens with a terminal showing its log).
launcher: build
	mkdir -p $(HOME)/.local/share/applications
	printf '[Desktop Entry]\nType=Application\nName=NetWatch\nComment=Network and spatial-sensing console\nExec=%s/$(BINDIR)/$(BINARY) gui\nIcon=%s/internal/webui/assets/favicon.png\nTerminal=true\nCategories=Network;Security;\nStartupWMClass=NetWatch\n' "$(CURDIR)" "$(CURDIR)" > $(HOME)/.local/share/applications/netwatch.desktop
	@echo "added NetWatch to the application menu"

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
