#!/bin/bash
# Build the static libraries that shaping/chb.go links: FreeType and HarfBuzz.
#
#   sh hb-build/build.sh macos
#   sh hb-build/build.sh android
#
# The libraries are per-machine artifacts and are not committed; this script
# downloads the sources and builds them with no external dependencies
# (HarfBuzz with glib, icu, cairo and its own FreeType integration disabled —
# the shaper reads tables itself).
#
# Requirements: a C toolchain and meson+ninja on PATH, plus an Android NDK for
# the android target. Point at the NDK with ANDROID_NDK_HOME, or let the script
# find it under the SDK's default location:
#
#   ANDROID_NDK_HOME=/path/to/ndk sh hb-build/build.sh android
#
set -euo pipefail
cd "$(dirname "$0")"

FREETYPE_VERSION=2.13.3
HARFBUZZ_VERSION=10.2.0
API=29

TARGET="${1:-}"
case "$TARGET" in
  macos | android) ;;
  *)
    echo "usage: $0 <macos|android>" >&2
    exit 2
    ;;
esac

SRC=src
# Sources are unpacked under src/ and the libraries installed to lib/<target>/,
# both gitignored; only this script is committed.
OUT="lib/$TARGET"
mkdir -p "$SRC" "$OUT"

if [ "$TARGET" = android ]; then
  NDK="${ANDROID_NDK_HOME:-${ANDROID_HOME:-$HOME/Library/Android/sdk}/ndk}"
  # Use the newest NDK present, whatever it is called.
  NDK=$(ls -d "$NDK"/* 2>/dev/null | sort -V | tail -1)
  if [ -z "${NDK:-}" ] || [ ! -d "$NDK" ]; then
    echo "no Android NDK found; set ANDROID_NDK_HOME" >&2
    exit 1
  fi
  HOST=$(ls -d "$NDK"/toolchains/llvm/prebuilt/* | head -1)
  CC="$HOST/bin/aarch64-linux-android$API-clang"
  CXX="$HOST/bin/aarch64-linux-android$API-clang++"
  HOST_FLAG=--host=aarch64-linux-android
  LDFLAGS_EXTRA="-llog"
else
  CC="clang -arch arm64"
  CXX="clang++ -arch arm64"
  HOST_FLAG=--host=aarch64-apple-darwin
  LDFLAGS_EXTRA=""
fi

fetch() {
  local url="$1" dir="$2"
  [ -d "$SRC/$dir" ] && return 0
  echo "downloading $dir"
  curl -fsSL "$url" -o "$SRC/$dir.tar.gz"
  tar -xzf "$SRC/$dir.tar.gz" -C "$SRC"
  rm -f "$SRC/$dir.tar.gz"
}

fetch "https://download.savannah.gnu.org/releases/freetype/freetype-$FREETYPE_VERSION.tar.gz" \
  "freetype-$FREETYPE_VERSION"
fetch "https://github.com/harfbuzz/harfbuzz/releases/download/$HARFBUZZ_VERSION/harfbuzz-$HARFBUZZ_VERSION.tar.xz" \
  "harfbuzz-$HARFBUZZ_VERSION"

PREFIX="$(cd "$OUT" && pwd)"

if [ ! -f "$OUT/libfreetype.a" ]; then
  (
    cd "$SRC/freetype-$FREETYPE_VERSION"
    CC="$CC" ./configure --prefix="$PREFIX" --disable-shared \
      --without-brotli --without-bzip2 --without-png --without-zlib --without-harfbuzz \
      "$HOST_FLAG" >/dev/null
    make -j"$(nproc 2>/dev/null || sysctl -n hw.ncpu)" >/dev/null
    make install >/dev/null
  )
  echo "freetype OK"
fi

if [ ! -f "$OUT/libharfbuzz.a" ]; then
  (
    cd "$SRC/harfbuzz-$HARFBUZZ_VERSION"
    CC="$CC" CXX="$CXX" meson setup "build-$TARGET" --prefix="$PREFIX" \
      -Dglib=disabled -Dicu=disabled -Dcairo=disabled -Dfreetype=disabled \
      -Dtests=disabled -Ddocs=disabled -Dbenchmark=disabled -Dintrospection=disabled \
      --default-library=static --buildtype=release >/dev/null
    ninja -C "build-$TARGET" install >/dev/null
  )
  echo "harfbuzz OK"
fi

ls -la "$OUT/lib"/*.a
echo
echo "Point a build at these with:"
echo "  CGO_CFLAGS=\"-I$PREFIX/include/harfbuzz -I$PREFIX/include/freetype2\" \\"
echo "  CGO_LDFLAGS=\"-L$PREFIX/lib -lharfbuzz -lfreetype -lm $LDFLAGS_EXTRA\""
