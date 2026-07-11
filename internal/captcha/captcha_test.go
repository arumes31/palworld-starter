package captcha

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
)

func TestNumberToWords(t *testing.T) {
	tests := []struct {
		num      int
		lang     string
		expected string
	}{
		// German tests
		{0, "de", "null"},
		{1, "de", "eins"},
		{12, "de", "zwölf"},
		{20, "de", "zwanzig"},
		{21, "de", "einsundzwanzig"},
		{99, "de", "neunundneunzig"},
		{100, "de", "einhundert"},
		{105, "de", "einhundertfünf"},
		{121, "de", "einhunderteinsundzwanzig"},
		{199, "de", "einhundertneunundneunzig"},

		// English tests
		{0, "en", "zero"},
		{1, "en", "one"},
		{12, "en", "twelve"},
		{20, "en", "twenty"},
		{21, "en", "twenty-one"},
		{99, "en", "ninety-nine"},
		{100, "en", "one hundred"},
		{105, "en", "one hundred five"},
		{121, "en", "one hundred twenty-one"},
		{199, "en", "one hundred ninety-nine"},
	}

	for _, tc := range tests {
		result := NumberToWords(tc.num, tc.lang)
		if result != tc.expected {
			t.Errorf("NumberToWords(%d, %q) = %q; expected %q", tc.num, tc.lang, result, tc.expected)
		}
	}
}

func TestGenerate(t *testing.T) {
	for i := 0; i < 100; i++ {
		for _, lang := range []string{"de", "en"} {
			ch := Generate(lang)
			if ch.Question == "" {
				t.Fatalf("%s captcha question should not be empty", lang)
			}
			if !strings.Contains(ch.Question, "{N1}") || !strings.Contains(ch.Question, "{N2}") {
				t.Errorf("question must contain both number placeholders: %q", ch.Question)
			}
			if ch.Num1 < 100 || ch.Num1 > 199 {
				t.Errorf("Num1 out of bounds: %d", ch.Num1)
			}
			if ch.Num2 < 1 || ch.Num2 > 99 {
				t.Errorf("Num2 out of bounds: %d", ch.Num2)
			}
			if ch.Answer != ch.Num1+ch.Num2 && ch.Answer != ch.Num1-ch.Num2 {
				t.Errorf("answer %d is neither %d+%d nor %d-%d", ch.Answer, ch.Num1, ch.Num2, ch.Num1, ch.Num2)
			}
			if ch.Answer < 1 || ch.Answer > 300 {
				t.Errorf("%s captcha answer out of bounds: %d", lang, ch.Answer)
			}
		}
	}
}

func TestSegments(t *testing.T) {
	ch := Generate("en")
	segs := ch.Segments()

	var rebuilt strings.Builder
	nums := map[int]int{}
	for _, seg := range segs {
		if seg.Num == 0 {
			rebuilt.WriteString(seg.Text)
		} else {
			nums[seg.Num]++
			rebuilt.WriteString("{N" + string(rune('0'+seg.Num)) + "}")
		}
	}
	if rebuilt.String() != ch.Question {
		t.Errorf("segments do not reconstruct question:\n got %q\nwant %q", rebuilt.String(), ch.Question)
	}
	if nums[1] == 0 || nums[2] == 0 {
		t.Errorf("expected segments for both numbers, got %v", nums)
	}
}

func TestRenderNumberPNG(t *testing.T) {
	small, err := RenderNumberPNG(7)
	if err != nil {
		t.Fatalf("RenderNumberPNG(7) failed: %v", err)
	}
	large, err := RenderNumberPNG(199)
	if err != nil {
		t.Fatalf("RenderNumberPNG(199) failed: %v", err)
	}

	imgSmall, err := png.Decode(bytes.NewReader(small))
	if err != nil {
		t.Fatalf("output is not a valid PNG: %v", err)
	}
	imgLarge, err := png.Decode(bytes.NewReader(large))
	if err != nil {
		t.Fatalf("output is not a valid PNG: %v", err)
	}

	if imgLarge.Bounds().Dx() <= imgSmall.Bounds().Dx() {
		t.Errorf("3-digit image (%dpx) should be wider than 1-digit image (%dpx)",
			imgLarge.Bounds().Dx(), imgSmall.Bounds().Dx())
	}
}
