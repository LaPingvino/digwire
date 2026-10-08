# Digwire

**This is the Go source repository for Digwire itself**, not a packaging directory. The `PKGBUILD`
and the `*.pkg.tar.zst` files in the root are build output of the Arch package built *from* this
source; the `.pkg.tar.zst` files are gitignored. The application lives in `cmd/digwire` and
`internal/`.

## Layout

- `cmd/digwire` — the binary: startup, single-instance lock, the native WebKitGTK window, logging.
- `internal/engine` — the torrent engine: sessions, verification, BEP 52 (v2/hybrid), alternate
  swarms, HTTP and media downloads. The biggest and most delicate package.
- `internal/web` — the HTTP API and the embedded interface (`internal/web/embedded`: plain HTML,
  CSS and JavaScript, no build step).
- `internal/dhtindex` — the local DHT index (SQLite), including metadata the crawler resolves.
- `internal/search`, `internal/config`, `internal/appdir` — indexer search, configuration, and
  where Digwire's own files live.
- `third_party/torrent` — a vendored fork of anacrolix/torrent, wired in by a `replace` in
  `go.mod`. Fixes to the library go here, with the reason in the commit message.

## Building and testing

```sh
go build ./...
go test ./... -count=1          # add -race when touching concurrency
```

Tests must never touch the user's real Digwire directory. `internal/appdir` enforces this: under
`go test` it hands out a throwaway directory, ignoring the inherited `XDG_CONFIG_HOME` (which on
the maintainer's machine points at the real `~/.config`). A test that wants its own directory sets
`XDG_CONFIG_HOME` itself. This is not theoretical: a test run once emptied the running app's
`session.json`.

## Installing a change on this machine

The `PKGBUILD` clones from GitHub, so **a commit that is not pushed will not be installed** — you
will quietly rebuild the previous version instead.

```sh
git commit ... && git push                       # push first, always
V="r$(git rev-list --count HEAD).$(git rev-parse --short HEAD)"
sed -i "s/^pkgver=.*/pkgver=$V/" PKGBUILD        # commit and push this bump too
rm -rf src/digwire-src                           # if makepkg's clone complains
makepkg -sfi --noconfirm                         # sudo is fine for this one
```

Restarting the app (never match the shell itself with `pgrep -f`):

```sh
kill -TERM "$(pgrep -x digwire)"
setsid nohup /usr/bin/digwire >/dev/null 2>&1 </dev/null & disown
```

## Running app

- Interface and API: <http://127.0.0.1:9091> (`/api/torrents`, `/api/health`, …).
- Log: `~/.cache/digwire/digwire.log` — always written, because a desktop launcher throws stderr
  away. Panics land here with their stack.
- `/api/health` takes no engine lock on purpose: it still answers when the engine is stuck, and
  reports panics and a stalled monitor loop. The interface shows both in a banner.
- `kill -USR1 "$(pgrep -x digwire)"` writes every goroutine's stack to the log; the same dump
  happens by itself when the engine stops ticking. That is how a freeze gets diagnosed.
- State lives in `~/.config/digwire`: `session.json` (the user's torrents), `torrents/` (cached
  metadata — **not** a list of the user's torrents), `dht_index.sqlite`, `.torrent.db`.

## House rules

- The user's data is sacred: never start, remove or overwrite torrents behind their back. Metadata
  Digwire happens to know about is *offered* (`FoundTorrents`), never added by itself.
- Deleting a torrent's files keeps files another torrent still uses.
- Comments explain why, not what, and the user reads the summaries: write them in plain language.
