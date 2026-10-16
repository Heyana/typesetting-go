//go:build android || darwin

package shaping

import (
	"os"
	"strings"
	"testing"

	"github.com/Heyana/typesetting-go/font"
	ot "github.com/Heyana/typesetting-go/font/opentype"
)

// Checks on wght: a variable font's weights needed these before any of them
// took effect.
//  1. wght 400 and 500 of one font differ in advance (the axis is applied)
//  2. CHB at 500 and the Go port at a 500 face shell agree on glyph IDs and
//     advances
func TestCHBVarWeight(t *testing.T) {
	path := os.Getenv("CHB_TEST_FONT")
	if path == "" {
		t.Skip("CHB_TEST_FONT is not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	base, err := font.ParseTTF(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !base.Font.HasVariations() {
		t.Skip("the test font is not a variable font")
	}
	shell := func(wght int) *font.Face {
		f := font.NewFace(base.Font)
		f.SetVariations([]font.Variation{{Tag: ot.MustNewTag("wght"), Value: float32(wght)}})
		return f
	}
	f400, f500 := shell(400), shell(500)

	chb := NewCHBShaper()
	chb.RegisterFace(f400, data, 0)
	// The 500 shell is registered too, as an engine registers every weight shell.
	chb.RegisterFace(f500, data, 0)
	var goShaper HarfbuzzShaper

	// Latin text: its advance changes visibly with wght, where a CJK square
	// has a fixed 1000em advance.
	text := []rune("Hello World rgqk")
	mk := func(fc *font.Face, w int) Input {
		return Input{Text: text, RunStart: 0, RunEnd: len(text), Face: fc, Size: 16 << 6, VarWght: w}
	}

	// 1. The variation takes effect: 400 and 500 differ in advance or extents.
	differs := func(x, y Output) bool {
		if len(x.Glyphs) != len(y.Glyphs) {
			return true
		}
		for i := range x.Glyphs {
			if x.Glyphs[i].Advance != y.Glyphs[i].Advance ||
				x.Glyphs[i].Width != y.Glyphs[i].Width ||
				x.Glyphs[i].Height != y.Glyphs[i].Height {
				return true
			}
		}
		return false
	}
	a400, a500 := chb.Shape(mk(f400, 400)), chb.Shape(mk(f500, 500))
	if !differs(a400, a500) {
		t.Fatal("wght 400 and 500 shape identically: the axis is not applied")
	}
	t.Log("the variation takes effect: 400 and 500 differ")

	// 2. CHB and the Go port agree at 500.
	g500, c500 := goShaper.Shape(mk(f500, 500)), chb.Shape(mk(f500, 500))
	if len(g500.Glyphs) != len(c500.Glyphs) {
		t.Fatalf("glyph count %d != %d", len(g500.Glyphs), len(c500.Glyphs))
	}
	for i := range g500.Glyphs {
		if g500.Glyphs[i].GlyphID != c500.Glyphs[i].GlyphID {
			t.Fatalf("glyph %d: ID %d != %d", i, g500.Glyphs[i].GlyphID, c500.Glyphs[i].GlyphID)
		}
		if d := float64(g500.Glyphs[i].Advance-c500.Glyphs[i].Advance) / 64; d > 0.5 || d < -0.5 {
			t.Fatalf("glyph %d: advance differs by %.2f", i, d)
		}
	}
	t.Log("CHB and the Go port are identical at wght 500")
}
