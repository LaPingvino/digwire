PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DATADIR ?= $(PREFIX)/share
APPDIR ?= $(DATADIR)/applications
ICONDIR ?= $(DATADIR)/icons/hicolor/512x512/apps

USER_PREFIX ?= $(HOME)/.local
USER_BINDIR ?= $(USER_PREFIX)/bin
USER_APPDIR ?= $(USER_PREFIX)/share/applications
USER_ICONDIR ?= $(USER_PREFIX)/share/icons/hicolor/512x512/apps

.PHONY: all build clean install install-user uninstall uninstall-user

all: build

build:
	go build -ldflags="-s -w" -o digwire ./cmd/digwire

clean:
	rm -f digwire

install: build
	install -d $(DESTDIR)$(BINDIR)
	install -d $(DESTDIR)$(APPDIR)
	install -d $(DESTDIR)$(ICONDIR)
	install -d $(DESTDIR)$(DATADIR)/pixmaps
	install -m 755 digwire $(DESTDIR)$(BINDIR)/digwire
	install -m 644 assets/digwire.desktop $(DESTDIR)$(APPDIR)/digwire.desktop
	install -m 644 assets/digwire.png $(DESTDIR)$(ICONDIR)/digwire.png
	install -m 644 assets/digwire.png $(DESTDIR)$(DATADIR)/pixmaps/digwire.png
	@which update-desktop-database >/dev/null 2>&1 && update-desktop-database $(DESTDIR)$(APPDIR) || true
	@which gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -f -t $(DESTDIR)$(DATADIR)/icons/hicolor || true

install-user: build
	install -d $(USER_BINDIR)
	install -d $(USER_APPDIR)
	install -d $(USER_ICONDIR)
	install -d $(USER_PREFIX)/share/pixmaps
	install -m 755 digwire $(USER_BINDIR)/digwire
	install -m 644 assets/digwire.desktop $(USER_APPDIR)/digwire.desktop
	install -m 644 assets/digwire.png $(USER_ICONDIR)/digwire.png
	install -m 644 assets/digwire.png $(USER_PREFIX)/share/pixmaps/digwire.png
	@which update-desktop-database >/dev/null 2>&1 && update-desktop-database $(USER_APPDIR) || true
	@which gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -f -t $(USER_PREFIX)/share/icons/hicolor || true

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/digwire
	rm -f $(DESTDIR)$(APPDIR)/digwire.desktop
	rm -f $(DESTDIR)$(ICONDIR)/digwire.png
	rm -f $(DESTDIR)$(DATADIR)/pixmaps/digwire.png

uninstall-user:
	rm -f $(USER_BINDIR)/digwire
	rm -f $(USER_APPDIR)/digwire.desktop
	rm -f $(USER_ICONDIR)/digwire.png
	rm -f $(USER_PREFIX)/share/pixmaps/digwire.png
