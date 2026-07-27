package reading

import "testing"

func TestParsePassagesURL(t *testing.T) {
	tests := []struct {
		name    string
		passage string
		want    string
	}{
		// Single-chapter books carry no reference in the plan, so chapter 1 is
		// added; without it sabda returns an empty page.
		{"obaja", "Ob.", "https://alkitab.sabda.org/passage.php?passage=Obaja+1"},
		{"filemon", "Flm.", "https://alkitab.sabda.org/passage.php?passage=Filemon+1"},
		{"2 yohanes", "2Yoh.", "https://alkitab.sabda.org/passage.php?passage=2+Yohanes+1"},
		{"3 yohanes", "3Yoh.", "https://alkitab.sabda.org/passage.php?passage=3+Yohanes+1"},
		{"yudas", "Yud.", "https://alkitab.sabda.org/passage.php?passage=Yudas+1"},

		// Existing references are left alone.
		{"yudas with verses", "Yud. 3-5", "https://alkitab.sabda.org/passage.php?passage=Yudas+3-5"},
		{"chapter only", "2Raj. 5", "https://alkitab.sabda.org/passage.php?passage=2+Raja-raja+5"},
		{"chapter and verses", "1Kor. 11:17-34", "https://alkitab.sabda.org/passage.php?passage=1+Korintus+11%3A17-34"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			links := ParsePassages(tt.passage)
			if len(links) != 1 {
				t.Fatalf("ParsePassages(%q) returned %d links, want 1", tt.passage, len(links))
			}
			if links[0].URL != tt.want {
				t.Errorf("URL = %q, want %q", links[0].URL, tt.want)
			}
			if links[0].Text != tt.passage {
				t.Errorf("Text = %q, want %q", links[0].Text, tt.passage)
			}
		})
	}
}

func TestParsePassagesSplitsParts(t *testing.T) {
	links := ParsePassages("1Kor. 11:17-34; 2Raj. 5; Ob.")
	if len(links) != 3 {
		t.Fatalf("got %d links, want 3", len(links))
	}
	if want := "https://alkitab.sabda.org/passage.php?passage=Obaja+1"; links[2].URL != want {
		t.Errorf("last URL = %q, want %q", links[2].URL, want)
	}
}
