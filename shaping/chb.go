//go:build android || darwin

// CHBShaper — a Shaper that shapes through C HarfBuzz (libharfbuzz.a).
//
// Why this replaces HarfbuzzShaper, the pure-Go port of HarfBuzz: the port
// parses and retains per-Face font tables (outline points, variation deltas),
// and a layout cache holding them costs tens of megabytes. C HarfBuzz reads
// the tables from the mmap on demand and retains nothing — the same idea as
// the lazy glyf and gvar tables here, taken to its end: the parsed result
// need not exist.
//
// The `android || darwin` build tag is there because this file links static
// FreeType and HarfBuzz libraries, which are per-machine artifacts. See
// hb-build/build.sh; the libraries land in hb-build/lib/<target>/.
//
// The ${SRCDIR}/../hb-build paths below are found when building inside this
// repository (this file is in shaping/, two levels under the root). Built as
// a dependency from the module cache they do not hold, since the package then
// lives under pkg/mod with no hb-build beside it, and the builder passes its
// own paths instead:
//
//	CGO_CFLAGS="-I$HB/include/harfbuzz -I$HB/include/freetype2"
//	CGO_LDFLAGS="-L$HB/lib -lharfbuzz -lfreetype -lm -llog"
//
// A ${VAR} of one's own cannot be used in the #cgo directives: cgo's parser
// knows only ${SRCDIR}, and rejects anything else with "malformed #cgo
// argument" (it also refuses a glued form such as -I${VAR}/x). The
// environment is the injection point Go itself supports, and CGO_CFLAGS is
// appended after the #cgo flags.
package shaping

/*
#cgo android CFLAGS: -I${SRCDIR}/../hb-build/lib/android/include/harfbuzz -I${SRCDIR}/../hb-build/lib/android/include/freetype2
#cgo android LDFLAGS: -L${SRCDIR}/../hb-build/lib/android/lib -lharfbuzz -lfreetype -lm -llog
#cgo darwin CFLAGS: -I${SRCDIR}/../hb-build/lib/macos/include/harfbuzz -I${SRCDIR}/../hb-build/lib/macos/include/freetype2
#cgo darwin LDFLAGS: -L${SRCDIR}/../hb-build/lib/macos/lib -lharfbuzz -lfreetype -lm
#include <hb.h>
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"

	"github.com/Heyana/typesetting-go/font"
	"golang.org/x/image/math/fixed"
)

// CHBShaper implements Shaper with C HarfBuzz. See the file comment.
type CHBShaper struct {
	// fonts caches the C-side hb_face (and its blob) for a face's underlying
	// font.Font. The bytes the blob refers to are held by the caller of
	// RegisterFace, on the Go side or by an mmap.
	fonts map[*font.Font]*hbFont
	// variants caches the hb_font that carries a variation, keyed by
	// (font, wght): the weights of one variable font share an hb_face and
	// each hb_font calls set_variations once. wght 0 or 400 uses the default
	// hb_font from fonts.
	variants map[variantKey]*C.hb_font_t
}

// variantKey is a font carrying a variation: one hb_font per wght of a font.
type variantKey struct {
	ft   *font.Font
	wght int
}

type hbFont struct {
	face *C.hb_face_t
	font *C.hb_font_t
	// src pins the font bytes the blob refers to. HarfBuzz reads them through
	// a READONLY pointer, so if the bytes became unreachable the collector
	// would take them and the C side would dangle, making hb_shape abort.
	// Bytes from an mmap point into the mapping, which is never unmapped;
	// heap bytes are kept alive by this reference.
	src []byte
}

// NewCHBShaper makes a C HarfBuzz shaper that caches no glyph data.
func NewCHBShaper() *CHBShaper {
	return &CHBShaper{fonts: map[*font.Font]*hbFont{}, variants: map[variantKey]*C.hb_font_t{}}
}

// RegisterFace hands a face's font source to C HarfBuzz: src is the whole
// font file (or an in-memory font) and index is the face within the
// collection. The blob refers to the bytes READONLY and copies nothing; their
// lifetime is the caller's to keep (an mmap or app data). A given font.Font
// is registered once; later calls do nothing.
func (t *CHBShaper) RegisterFace(f *font.Face, src []byte, index int) {
	if f == nil || f.Font == nil {
		return
	}
	if _, ok := t.fonts[f.Font]; ok {
		return
	}
	var blob *C.hb_blob_t
	if len(src) > 0 {
		blob = C.hb_blob_create((*C.char)(unsafe.Pointer(&src[0])), C.uint(len(src)),
			C.HB_MEMORY_MODE_READONLY, nil, nil)
	} else {
		return
	}
	cface := C.hb_face_create(blob, C.uint(index))
	C.hb_blob_destroy(blob) // the face holds its own reference; ownership moves
	// An empty face: HarfBuzz does not read a WOFF container (no brotli or
	// zlib) and reports a glyph count of 0. Such a face is not registered —
	// Shape falls back to the Go port, which can decode WOFF. Registering it
	// would give every glyph id 0, which loses the whole page's text.
	if C.hb_face_get_glyph_count(cface) == 0 {
		C.hb_face_destroy(cface)
		return
	}
	cfont := C.hb_font_create(cface)
	upem := int(C.hb_face_get_upem(cface))
	C.hb_font_set_scale(cfont, C.int(upem), C.int(upem))
	t.fonts[f.Font] = &hbFont{face: cface, font: cfont, src: src}
}

// Shape shapes one run with C HarfBuzz, producing an Output of the same shape
// as HarfbuzzShaper.Shape: clusters, direction and sizes match item by item
// (see countClusters in this package).
func (t *CHBShaper) Shape(input Input) Output {
	runes, start, end := input.Text, input.RunStart, input.RunEnd
	if end < start {
		end, start = start, end
	}
	start = clamp(start, 0, len(runes))
	end = clamp(end, 0, len(runes))

	// Route by wght: the weights of one variable font share an hb_face, and
	// each wght gets an hb_font that calls set_variations once.
	base, ok := t.fonts[input.Face.Font]
	if !ok {
		// An unregistered face falls back to the Go port, which should not
		// be the common case.
		var fallback HarfbuzzShaper
		return fallback.Shape(input)
	}
	hfont := base.font
	if w := input.VarWght; w != 0 && w != 400 {
		k := variantKey{ft: input.Face.Font, wght: w}
		if vf, ok := t.variants[k]; ok {
			hfont = vf
		} else {
			vf := C.hb_font_create(base.face)
			C.hb_font_set_scale(vf, C.int(int32(input.Size)), C.int(int32(input.Size)))
			// Build HB_TAG('w','g','h','t') by value, rather than through
			// hb_variation_from_string and a temporary Go string.
			v := C.hb_variation_t{tag: 0x77676874, value: C.float(w)}
			C.hb_font_set_variations(vf, &v, 1)
			t.variants[k] = vf
			hfont = vf
		}
	}
	hf := &hbFont{face: base.face, font: hfont}

	buf := C.hb_buffer_create()
	defer C.hb_buffer_destroy(buf)
	C.hb_buffer_pre_allocate(buf, C.uint(end-start))

	// runes to UTF-32. The whole text is added, with item_offset and
	// item_length picking the run: HarfBuzz assigns each cluster an index
	// into the array it was given, so only the whole text yields the
	// absolute rune indices the Go port produces, which countClusters, line
	// breaking and every downstream comparison rely on.
	u32 := make([]C.uint32_t, len(runes))
	for i, r := range runes {
		u32[i] = C.uint32_t(r)
	}
	if len(u32) > 0 && end > start {
		C.hb_buffer_add_utf32(buf, &u32[0], C.int(len(u32)), C.uint(start), C.int(end-start))
	}

	// Direction: the Go port's Direction values are the same as C HarfBuzz's.
	hbdir := input.Direction.Harfbuzz()
	C.hb_buffer_set_direction(buf, C.hb_direction_t(uint32(hbdir)))
	if len(input.Language) > 0 {
		C.hb_buffer_set_language(buf, C.hb_language_from_string(cstr(string(input.Language)), C.int(len(input.Language))))
	}
	if s := input.Script.String(); len(s) == 4 {
		C.hb_buffer_set_script(buf, C.hb_script_from_string(cstr(s), 4))
	}

	// Scale: input.Size is already 26.6 (px * 64), and HarfBuzz computes a
	// position as units * scale / upem, so setting scale to Size gives a
	// 26.6 position directly — the same as the Go port's XScale = size << 6
	// followed by >> 6. It must not be Ceil(), which is a pixel count and
	// would shrink a whole page to a sixty-fourth of its size.
	size26 := int32(input.Size)
	C.hb_font_set_scale(hf.font, C.int(size26), C.int(size26))

	// features: a FontFeature is only a tag and a value, and applies
	// everywhere in the run.
	var cfeats []C.hb_feature_t
	for _, f := range input.FontFeatures {
		cfeats = append(cfeats, C.hb_feature_t{
			tag:   C.hb_tag_t(f.Tag),
			value: C.uint(f.Value),
			start: 0,
			end:   C.uint(0xFFFFFFFF),
		})
	}
	var featsPtr *C.hb_feature_t
	if len(cfeats) > 0 {
		featsPtr = &cfeats[0]
	}
	C.hb_shape(hf.font, buf, featsPtr, C.uint(len(cfeats)))

	var nInf, nPos C.uint
	infos := C.hb_buffer_get_glyph_infos(buf, &nInf)
	poss := C.hb_buffer_get_glyph_positions(buf, &nPos)
	gi := unsafe.Slice((*C.hb_glyph_info_t)(unsafe.Pointer(infos)), int(nInf))
	gp := unsafe.Slice((*C.hb_glyph_position_t)(unsafe.Pointer(poss)), int(nPos))

	isVertical := input.Direction.IsVertical()
	glyphs := make([]Glyph, len(gi))
	for i := range glyphs {
		glyphs[i] = Glyph{
			ClusterIndex: int(gi[i].cluster),
			GlyphID:      gID(gi[i].codepoint),
			Mask:         uint32(gi[i].mask),
		}
		if isVertical {
			glyphs[i].YAdvance = fixed26(int(gp[i].y_advance))
			glyphs[i].Advance = glyphs[i].YAdvance
		} else {
			glyphs[i].XAdvance = fixed26(int(gp[i].x_advance))
			glyphs[i].Advance = glyphs[i].XAdvance
		}
		glyphs[i].XOffset = fixed26(int(gp[i].x_offset))
		glyphs[i].YOffset = fixed26(int(gp[i].y_offset))

		// Outline metrics, taken from C HarfBuzz on demand: read from the mmap,
		// nothing resident.
		var ext C.hb_glyph_extents_t
		if C.hb_font_get_glyph_extents(hf.font, C.hb_codepoint_t(gi[i].codepoint), &ext) != 0 {
			glyphs[i].Width = fixed26(int(ext.width))
			glyphs[i].Height = fixed26(int(ext.height))
			glyphs[i].XBearing = fixed26(int(ext.x_bearing))
			glyphs[i].YBearing = fixed26(int(ext.y_bearing))
		}
	}
	countClusters(glyphs, input.RunEnd, input.Direction.Progression())
	out := Output{
		Glyphs:    glyphs,
		Direction: input.Direction,
		Face:      input.Face,
		Size:      input.Size,
	}
	out.Runes.Offset = input.RunStart
	out.Runes.Count = input.RunEnd - input.RunStart

	if input.Direction.IsSideways() {
		out.sideways()
	}

	// Line bounds: the C side takes the font-level extents for the direction.
	var fext C.hb_font_extents_t
	hdir := C.hb_direction_t(uint32(hbdir))
	C.hb_font_get_extents_for_direction(hf.font, hdir, &fext)
	out.LineBounds = Bounds{
		Ascent:  fixed26(int(fext.ascender)),
		Descent: fixed26(int(fext.descender)),
		Gap:     fixed26(int(fext.line_gap)),
	}
	out.RecalculateAll()
	replaceNotSupportedSpaces(input.Text, out.Glyphs)
	return out
}

// gID converts a C codepoint (a glyph ID after shaping) to this package's type.
func gID(cp C.hb_codepoint_t) font.GID { return font.GID(cp) }

func cstr(s string) *C.char { return (*C.char)(unsafe.Pointer(unsafe.StringData(s))) }

// fixed26 makes a fixed.Int26_6 of an integer. go.mod locks go 1.19, where
// unsafe.StringData (1.20) is unavailable, so the fixed type is built through
// golang.org/x/image/math/fixed's own Int26_6 rather than the std one.
func fixed26(v int) fixed.Int26_6 { return fixed.Int26_6(v) }
