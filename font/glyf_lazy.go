package font

import (
	"github.com/Heyana/typesetting-go/font/opentype/tables"
)

// Keep no parsed glyf table. Upstream parses the whole table into a []Glyph
// and holds it for the life of the Font (6.2 MB on a CJK variable font). An
// outline is a pure function of the raw bytes, so parse one glyph when it is
// asked for and let the garbage collector take it; what deserves to stay
// resident is the rasterized bitmap, which a toolkit can budget itself. The
// Font keeps the raw table and the loca offsets (kilobytes).

// glyfAt parses one glyph and returns it. Nothing is retained: the caller
// gets a value, and the parse is garbage afterwards.
//
// The loca offsets decide what exists, exactly as upstream's ParseGlyf does:
// start == end is a glyph with no outline (a space), which is a zero Glyph
// and not an error. glyfRaw may be nil or empty while loca is not — a font
// whose glyf table is missing still has offsets, and upstream yields that
// many zero glyphs for it.
func (f *Font) glyfAt(gid gID) (tables.Glyph, bool) {
	if f.glyfOverride != nil {
		return f.glyfOverride(gid)
	}
	if int(gid) >= len(f.loca)-1 {
		return tables.Glyph{}, false
	}
	start, end := f.loca[gid], f.loca[gid+1]
	if start >= end {
		// No outline, as upstream's ParseGlyf leaves it: a zero Glyph.
		return tables.Glyph{}, true
	}
	if int(end) > len(f.glyfRaw) {
		return tables.Glyph{}, false
	}
	g, _, err := tables.ParseGlyph(f.glyfRaw[start:end])
	if err != nil {
		return tables.Glyph{}, false
	}
	return g, true
}

// hasGlyf reports whether the font has glyf outlines, as upstream's
// `f.glyf != nil` did. The offsets are what says so, not the raw table.
func (f *Font) hasGlyf() bool { return len(f.loca) > 0 }

// numGlyfGlyphs is how many glyphs the glyf table covers, as upstream's
// `len(f.glyf)` was.
func (f *Font) numGlyfGlyphs() int {
	if len(f.loca) == 0 {
		return 0
	}
	return len(f.loca) - 1
}
