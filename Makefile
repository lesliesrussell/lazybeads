# lb-98h
PREFIX  ?= /usr/local
BINDIR  ?= $(PREFIX)/bin
DESTDIR ?=
GO      ?= go

.PHONY: all build install uninstall test clean help

all: build

help:
	@echo "Targets:"
	@echo "  make build      compile bin/lb"
	@echo "  make install    copy lb to $(BINDIR)"
	@echo "  make uninstall  remove $(BINDIR)/lb"
	@echo "  make test       go test ./..."
	@echo "  make clean      remove bin/"

build:
	mkdir -p bin
	$(GO) build -o bin/lb ./cmd/lb

install: build
	install -d $(DESTDIR)$(BINDIR)
	install -m 755 bin/lb $(DESTDIR)$(BINDIR)/lb

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/lb

test:
	$(GO) test ./...

clean:
	rm -rf bin
