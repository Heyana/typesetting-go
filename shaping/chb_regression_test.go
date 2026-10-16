//go:build android || darwin

package shaping

import (
	"os"
	"strings"
	"testing"

	"github.com/Heyana/typesetting/di"
	"github.com/Heyana/typesetting/font"
)

// Correctness regression for the move to C HarfBuzz: glyph by glyph against
// the Go port. The cases are the hardest paths a shaper integration can get
// wrong: RTL, bidi mixing, a run that does not start at 0 (cross-run
// context), and mixing at a variation weight.
func TestCHBRegression(t *testing.T) {
	path := os.Getenv("CHB_TEST_FONT")
	if path == "" {
		t.Skip("CHB_TEST_FONT is not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	face, err := font.ParseTTF(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	chb := NewCHBShaper()
	chb.RegisterFace(face, data, 0)
	var goShaper HarfbuzzShaper

	cases := []struct {
		name string
		text []rune
		dir  di.Direction
		wght int
	}{
		{"latin", []rune("Hello World rgqk 123"), di.DirectionLTR, 0},
		{"rtl-hebrew", []rune("שלום עולם מה קורה"), di.DirectionRTL, 0},
		{"rtl-arabic", []rune("مرحبا بالعالم"), di.DirectionRTL, 0},
		{"bidi-mixed", []rune("性能 Hello שלום 优化 123"), di.DirectionLTR, 0},
		{"rtl-with-latin", []rune("مرحبا Hello 123 بالعالم"), di.DirectionRTL, 0},
		{"cjk-weight500", []rune("性能优化的第一性原理"), di.DirectionLTR, 500},
		{"latin-weight700", []rune("Hello World bold"), di.DirectionLTR, 700},
		{"mid-run-context", []rune("0123性能优化的第一性原理4567"), di.DirectionLTR, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := 0, len(tc.text)
			if tc.name == "mid-run-context" { // a run not starting at 0: keep context on both sides
				start, end = 4, len(tc.text)-4
			}
			mk := func() Input {
				return Input{Text: tc.text, RunStart: start, RunEnd: end,
					Face: face, Size: 16 << 6, VarWght: tc.wght,
					Direction: tc.dir}
			}
			got, want := chb.Shape(mk()), goShaper.Shape(mk())
			if len(got.Glyphs) != len(want.Glyphs) {
				t.Fatalf("glyph count %d != %d", len(got.Glyphs), len(want.Glyphs))
			}
			for i := range want.Glyphs {
				g, w := got.Glyphs[i], want.Glyphs[i]
				if g.GlyphID != w.GlyphID {
					t.Fatalf("glyph %d: ID %d != %d", i, g.GlyphID, w.GlyphID)
				}
				if g.ClusterIndex != w.ClusterIndex {
					t.Fatalf("glyph %d: cluster %d != %d", i, g.ClusterIndex, w.ClusterIndex)
				}
				// Tolerance of 1px: C HarfBuzz rounds an advance (of a space, say)
				// while the Go port truncates (>> 6). Sub-pixel, and invisible in
				// a UI.
				if d := float64(g.Advance-w.Advance) / 64; d > 1 || d < -1 {
					t.Fatalf("glyph %d: advance differs by %.2f", i, d)
				}
				if d := float64(g.XOffset-w.XOffset) / 64; d > 1 || d < -1 {
					t.Fatalf("glyph %d: xoffset differs by %.2f", i, d)
				}
			}
			if got.LineBounds.Ascent != want.LineBounds.Ascent ||
				got.LineBounds.Descent != want.LineBounds.Descent {
				t.Fatalf("LineBounds differ: %v != %v", got.LineBounds, want.LineBounds)
			}
		})
	}
}
