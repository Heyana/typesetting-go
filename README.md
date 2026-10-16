# typesetting

This is a fork of [go-text/typesetting](https://github.com/go-text/typesetting)
v0.3.5 that keeps far less of a font in memory. It exists for toolkits that
shape text in Go on an operating system whose own text stack is out of reach
— Android, where Minikin and Skia are not part of the NDK.

Two changes carry it. Both follow one idea, taken from HarfBuzz: **the parsed
result need not exist.** An outline is a pure function of the raw bytes, so
parse it when asked and let the garbage collector take it; what deserves to
stay resident is the rasterized bitmap, which a toolkit can budget itself.

- **Lazy `glyf` and `gvar`.** Upstream parses a table once and holds the Go
  objects for the life of the `Font`. Here the `Font` keeps raw bytes and
  `loca` only, and glyphs are parsed on each access (`font/glyf_lazy.go`,
  `unpack` in `font/variations.go`). For a CJK variable font this drops a
  6.2 MB parsed `Glyf`, an 8.2 MB `ParseGvar`, and 7.3 MB of table copies.
  Upstream's contract is kept: `NewFont` still reports a malformed `gvar`,
  though for fonts above `validateMaxGlyphs` the deep per-glyph check is
  skipped, since it would unpack the whole table once per fallback face.

- **C HarfBuzz shaper** (`shaping/chb.go`, `android || darwin`). The Go
  HarfBuzz port parses and retains per-`Face` tables — points and variation
  deltas — and a layout cache holding them costs ~10–15 MB. C HarfBuzz reads
  tables from the mmap on demand and retains nothing.

The two are the same disease in two forms: a Go-side parse result kept alive.

## Building the C HarfBuzz shaper

`shaping/chb.go` links FreeType and HarfBuzz **static libraries**, which are
per-machine artifacts and are not committed. `hb-build/build.sh` downloads and
builds them:

```sh
sh hb-build/build.sh macos     # hb-build/lib/macos/*.a
sh hb-build/build.sh android   # hb-build/lib/android/*.a; needs an NDK
```

It needs a C toolchain and `meson`+`ninja` on PATH, and for Android an NDK —
found under the SDK, or set with `ANDROID_NDK_HOME`.

Inside this repository `chb.go` finds the libraries by relative path. As a
dependency built from the module cache that path does not hold, so the build
passes its own; cgo accepts only `${SRCDIR}` in `#cgo` directives and rejects
anything else with `malformed #cgo argument`, so the injection point is the
environment:

```sh
HB=/path/to/hb-build/lib/android
CGO_CFLAGS="-I$HB/include/harfbuzz -I$HB/include/freetype2" \
CGO_LDFLAGS="-L$HB/lib -lharfbuzz -lfreetype -lm -llog" \
GOOS=android GOARCH=arm64 CGO_ENABLED=1 go build ./...
```

Without those libraries the package is not built: `chb.go` is tagged
`android || darwin` and the pure-Go shaper is used elsewhere.

## Upstream

The rest of this library is upstream's, unchanged — its API, its shaper, its
segmenter, its documentation below. Upstream is the work of
[go-text](https://github.com/go-text) and is shared by Fyne, Gio and
Ebitengine. Its license is kept here unchanged: `Unlicense OR BSD-3-Clause`
(see LICENSE).

---

This library provides typesetting capabilities in pure Go. It is appropriate for use in GUI applications, and is shared by multiple Go UI toolkits including [Fyne](https://fyne.io), [Gio](https://gioui.org), and [Ebitengine](https://ebitengine.org).

## Development cycle

This project, although already used in production by UI toolkits, still evolves rapidly. As such, the library uses unstable versions v0.x.y : the required breaking changes will bump the minor version number (x); the bug fixes and performance improvements the patch number (y).

## Review guidelines
 
Go-text is a collaboration between many individuals and projects, it is important to us that
designs and decisions are right for the broadest possible audience.
As a result the project will always have 3 maintainers that represent different projects
(currently Fyne.io, Gio and an independent developer).

### API and Architectural decisions

Changes to any core go-text repositories (not including utility or generator repos) will require
sign-off from at least 2 of these 3 maintainers to be approved.
"typesetting" and "render" are currently considered core.
Upon approval the second thumbs up will typically merge the change into the repository.

Decisions or API discussions are best carried out within the context of a GitHub issue or
pull request for greatest visibility in the future.

### Smaller changes and quality of life improvements

For speed of acceptance on smaller issues it is not always required to have complete consensus.
When a change is deemed to be of minor impact (for example documentation corrections, trivial
bug fixes and straight forward refactoring of content) an expedited review is supported.
In this situation the contribution only requires a single approval (not from the individual
proposing the change).

If in doubt please seek approval of two maintainers - and feel free to ask questions in the
#go-text channel of gophers Slack server.
