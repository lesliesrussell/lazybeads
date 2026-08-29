# lb-98h
PREFIX       ?= /usr/local
BINDIR       ?= $(PREFIX)/bin
MANDIR       ?= $(PREFIX)/share/man/man1
BASHCOMPDIR  ?= $(PREFIX)/share/bash-completion/completions
ZSHCOMPDIR   ?= $(PREFIX)/share/zsh/site-functions
FISHCOMPDIR  ?= $(PREFIX)/share/fish/vendor_completions.d
DESTDIR      ?=
GO           ?= go

.PHONY: all build install uninstall test clean help

all: build

help:
	@echo "Targets:"
	@echo "  make build      compile bin/lb"
	@echo "  make install    copy lb, man page, and completions under $(PREFIX)"
	@echo "  make uninstall  remove installed files"
	@echo "  make test       go test ./..."
	@echo "  make clean      remove bin/ and generated man/"

build:
	mkdir -p bin
	$(GO) build -o bin/lb ./cmd/lb

# lb-wlu
install: build
	install -d $(DESTDIR)$(BINDIR)
	install -m 755 bin/lb $(DESTDIR)$(BINDIR)/lb
	install -d $(DESTDIR)$(MANDIR)
	./bin/lb man > $(DESTDIR)$(MANDIR)/lb.1
	install -d $(DESTDIR)$(BASHCOMPDIR)
	./bin/lb completion bash > $(DESTDIR)$(BASHCOMPDIR)/lb
	install -d $(DESTDIR)$(ZSHCOMPDIR)
	./bin/lb completion zsh > $(DESTDIR)$(ZSHCOMPDIR)/_lb
	install -d $(DESTDIR)$(FISHCOMPDIR)
	./bin/lb completion fish > $(DESTDIR)$(FISHCOMPDIR)/lb.fish

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/lb
	rm -f $(DESTDIR)$(MANDIR)/lb.1
	rm -f $(DESTDIR)$(BASHCOMPDIR)/lb
	rm -f $(DESTDIR)$(ZSHCOMPDIR)/_lb
	rm -f $(DESTDIR)$(FISHCOMPDIR)/lb.fish

test:
	$(GO) test ./...

clean:
	rm -rf bin
