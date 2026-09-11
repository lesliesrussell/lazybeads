# lb-98h
# lb-m3o: default to the per-user prefix. It needs no sudo and ~/.local/bin is
# what the shell actually resolves lb to; PREFIX=/usr/local still works for a
# system-wide install.
PREFIX       ?= $(HOME)/.local
BINDIR       ?= $(PREFIX)/bin
MANDIR       ?= $(PREFIX)/share/man/man1
BASHCOMPDIR  ?= $(PREFIX)/share/bash-completion/completions
ZSHCOMPDIR   ?= $(PREFIX)/share/zsh/site-functions
FISHCOMPDIR  ?= $(PREFIX)/share/fish/vendor_completions.d
DESTDIR      ?=
GO           ?= go

.PHONY: all build man install uninstall test clean help release-check

all: build

help:
	@echo "Targets:"
	@echo "  make build      compile bin/lb"
	@echo "  make man        render man/lb.1 for local inspection"
	@echo "  make install    copy lb, man page, and completions under $(PREFIX)"
	@echo "                  override with: make install PREFIX=/usr/local (needs sudo)"
	@echo "  make uninstall  remove installed files"
	@echo "  make test       go test ./..."
	@echo "  make release-check  goreleaser check"
	@echo "  make clean      remove bin/ and generated man/"

build:
	mkdir -p bin
	$(GO) build -o bin/lb ./cmd/lb

# lb-b4p: the page is embedded in the binary, so rendering it is just a run.
# Useful for `man -l man/lb.1` and `mandoc -T lint man/lb.1` before a release.
man: build
	mkdir -p man
	./bin/lb man > man/lb.1

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
	@echo
	@echo "Installed lb.1 to $(DESTDIR)$(MANDIR)."
	@echo "If 'man lb' does not find it, $(PREFIX)/share/man is not on your"
	@echo "MANPATH; add it to your shell profile:"
	@echo '  export MANPATH="$(PREFIX)/share/man:$$MANPATH"'

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/lb
	rm -f $(DESTDIR)$(MANDIR)/lb.1
	rm -f $(DESTDIR)$(BASHCOMPDIR)/lb
	rm -f $(DESTDIR)$(ZSHCOMPDIR)/_lb
	rm -f $(DESTDIR)$(FISHCOMPDIR)/lb.fish

test:
	$(GO) test ./...

# lb-uvj
release-check:
	goreleaser check

clean:
	rm -rf bin man
