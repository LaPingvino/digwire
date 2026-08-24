# Maintainer: Joop Kiefte <joop@kiefte.eu>
pkgname=digwire-git
_pkgname=digwire
pkgver=r23.a24edb5
pkgrel=1
pkgdesc="Modern hybrid BitTorrent swarm & multi-source web download manager with Libadwaita aesthetic"
arch=('x86_64' 'aarch64' 'armv7h')
url="https://github.com/LaPingvino/digwire"
license=('GPL-3.0-or-later')
depends=('glibc')
makedepends=('go' 'git')
provides=('digwire')
conflicts=('digwire')
source=("$_pkgname-src::git+https://github.com/LaPingvino/digwire.git")
sha256sums=('SKIP')

pkgver() {
  cd "$srcdir/$_pkgname-src"
  printf "r%s.%s" "$(git rev-list --count HEAD)" "$(git rev-parse --short HEAD)"
}

build() {
  cd "$srcdir/$_pkgname-src"
  export CGO_CPPFLAGS="${CPPFLAGS}"
  export CGO_CFLAGS="${CFLAGS}"
  export CGO_CXXFLAGS="${CXXFLAGS}"
  export CGO_LDFLAGS="${LDFLAGS}"
  export GOFLAGS="-buildmode=pie -trimpath -modcacherw"
  go build -ldflags="-s -w -X main.version=$pkgver" -o digwire ./cmd/digwire
}

package() {
  cd "$srcdir/$_pkgname-src"
  install -Dm755 digwire "$pkgdir/usr/bin/digwire"
  install -Dm644 assets/digwire.desktop "$pkgdir/usr/share/applications/digwire.desktop"
  install -Dm644 assets/digwire.png "$pkgdir/usr/share/icons/hicolor/512x512/apps/digwire.png"
  install -Dm644 README.md "$pkgdir/usr/share/doc/digwire/README.md"
}
