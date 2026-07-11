package captcha

import (
	"bytes"
	"fmt"
	"image/png"
	"regexp"
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
		{21, "de", "einundzwanzig"},
		{99, "de", "neunundneunzig"},
		{100, "de", "einhundert"},
		{105, "de", "einhundertfünf"},
		{121, "de", "einhunderteinundzwanzig"},
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
	for i := 0; i < 500; i++ {
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
			if ch.Num2 < 2 || ch.Num2 > 99 {
				t.Errorf("Num2 out of bounds: %d", ch.Num2)
			}
			// The question may ask for the result, the change or the start.
			valid := map[int]bool{
				ch.Num1 + ch.Num2: true,
				ch.Num1 - ch.Num2: true,
				ch.Num1:           true,
				ch.Num2:           true,
			}
			if !valid[ch.Answer] {
				t.Errorf("answer %d does not match any askable quantity for %d/%d", ch.Answer, ch.Num1, ch.Num2)
			}
			if ch.Answer < 1 || ch.Answer > 300 {
				t.Errorf("%s captcha answer out of bounds: %d", lang, ch.Answer)
			}
			// Story sentence 1, at least one filler sentence, story
			// sentence 2 - the numbers must never be in adjacent sentences.
			if strings.Count(ch.Question, ".") < 3 {
				t.Errorf("question must contain at least three sentences: %q", ch.Question)
			}
			between := ch.Question[strings.Index(ch.Question, "{N1}"):strings.Index(ch.Question, "{N2}")]
			if strings.Count(between, ".") < 2 {
				t.Errorf("expected a filler sentence between the numbers: %q", ch.Question)
			}
			if ch.Fingerprint == "" {
				t.Errorf("challenge must carry a fingerprint")
			}
			// No leftover placeholders or masculine pronoun leaks next to
			// female actors ("Die X ... er" would be a feminize failure).
			if strings.Contains(ch.Question, "{A}") || strings.Contains(ch.Question, "{S}") {
				t.Errorf("unreplaced middle placeholder in %q", ch.Question)
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

// TestContentLint validates the whole content corpus so that agent- or
// human-added stories cannot silently break grammar or formatting rules.
func TestContentLint(t *testing.T) {
	dativeMarker := regexp.MustCompile(`\b(mit|bei|von|aus|zu) %s %s`)
	// The infinitive "sein" would be feminized into "ihr"; possessives like
	// "zu seiner" are fine, so match the exact word only.
	infinitiveSein := regexp.MustCompile(`\bzu sein\b`)

	checkPack := func(lang, tone string, pack tonePack) {
		prefix := fmt.Sprintf("%s/%s", lang, tone)

		if len(pack.Themes) == 0 || len(pack.Intros) == 0 || len(pack.Middles) == 0 ||
			len(pack.Add) == 0 || len(pack.Sub) == 0 {
			t.Errorf("%s: pack has empty pools", prefix)
		}

		for _, th := range pack.Themes {
			if th.Gender != "m" && th.Gender != "f" {
				t.Errorf("%s theme %q: invalid gender %q", prefix, th.Actor, th.Gender)
			}
			if th.Actor == "" || th.Item == "" || th.ItemDative == "" || th.Setting == "" {
				t.Errorf("%s theme %q: empty field", prefix, th.Actor)
			}
			if lang == "en" && th.ItemDative != th.Item {
				t.Errorf("%s theme %q: English ItemDative must equal Item", prefix, th.Actor)
			}
			if lang == "de" {
				// German dative plural: unchanged when the plural already
				// ends in -n or -s, otherwise Item+"n".
				endsNS := strings.HasSuffix(th.Item, "n") || strings.HasSuffix(th.Item, "s")
				if endsNS && th.ItemDative != th.Item {
					t.Errorf("%s theme %q: dative of %q must be unchanged", prefix, th.Actor, th.Item)
				}
				if !endsNS && th.ItemDative != th.Item+"n" {
					t.Errorf("%s theme %q: dative of %q must be %q, got %q", prefix, th.Actor, th.Item, th.Item+"n", th.ItemDative)
				}
			}
		}

		for _, intro := range pack.Intros {
			if strings.Count(intro, "%s") != 2 {
				t.Errorf("%s intro %q: must have exactly two %%s slots", prefix, intro)
			}
		}

		for _, m := range pack.Middles {
			if !strings.HasSuffix(m, ".") {
				t.Errorf("%s middle %q: must end with a period", prefix, m)
			}
			if strings.ContainsAny(m, "0123456789%") {
				t.Errorf("%s middle %q: must not contain digits or format verbs", prefix, m)
			}
			if lang == "de" && infinitiveSein.MatchString(m) {
				// The feminize pass would turn the infinitive into "zu ihr".
				t.Errorf("%s middle %q: must not use the infinitive 'sein'", prefix, m)
			}
		}

		lintTemplates := func(kind string, tmpls []storyTmpl) {
			for _, st := range tmpls {
				want := 4
				if st.NamesItem {
					want = 5
				}
				if got := strings.Count(st.Format, "%s"); got != want {
					t.Errorf("%s %s template %q: %d %%s slots, want %d", prefix, kind, st.Format, got, want)
				}
				if !strings.HasSuffix(st.Format, ".") {
					t.Errorf("%s %s template %q: must end with a period", prefix, kind, st.Format)
				}
				if strings.Count(st.Format, ".") != 2 {
					t.Errorf("%s %s template %q: must consist of exactly two sentences", prefix, kind, st.Format)
				}
				if lang == "de" {
					hasMarker := dativeMarker.MatchString(st.Format)
					if hasMarker && !st.FirstDative {
						t.Errorf("%s %s template %q: dative preposition before item slot but FirstDative is false", prefix, kind, st.Format)
					}
					if !hasMarker && st.FirstDative {
						t.Errorf("%s %s template %q: FirstDative set but no dative preposition found", prefix, kind, st.Format)
					}
					if infinitiveSein.MatchString(st.Format) {
						t.Errorf("%s %s template %q: must not use the infinitive 'sein'", prefix, kind, st.Format)
					}
				}
			}
		}
		lintTemplates("add", pack.Add)
		lintTemplates("sub", pack.Sub)
	}

	checkPack("de", "classic", classicDe)
	checkPack("de", "horror", horrorDe)
	checkPack("en", "classic", classicEn)
	checkPack("en", "horror", horrorEn)

	for _, qs := range [][]string{
		questionsResultAddDe, questionsResultSubDe, questionsDeltaAddDe, questionsDeltaSubDe, questionsStartDe,
		questionsResultAddEn, questionsResultSubEn, questionsDeltaAddEn, questionsDeltaSubEn, questionsStartEn,
	} {
		for _, q := range qs {
			if strings.Count(q, "%s") != 1 {
				t.Errorf("question %q: must have exactly one %%s slot", q)
			}
			if !strings.HasSuffix(q, "?") {
				t.Errorf("question %q: must end with a question mark", q)
			}
		}
	}
}

func TestFeminize(t *testing.T) {
	tests := []struct {
		lang string
		in   string
		want string
	}{
		{"de", "Er verschenkt drei davon, denn ihm gehören viele.", "Sie verschenkt drei davon, denn ihr gehören viele."},
		{"de", "Die Hexe prüft sein Gepäck und nennt es sein Eigen.", "Die Hexe prüft ihr Gepäck und nennt es ihr Eigen."},
		{"de", "Sie wollen ihn verlieren.", "Sie wollen sie verlieren."},
		{"en", "He checks his gear, and a raven watches him.", "She checks her gear, and a raven watches her."},
	}
	for _, tc := range tests {
		if got := feminize(tc.in, tc.lang); got != tc.want {
			t.Errorf("feminize(%q, %s) = %q; want %q", tc.in, tc.lang, got, tc.want)
		}
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
