package production

import "testing"

func TestCountWords(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{"", 0},
		{"Kaygı nedir?", 2},
		{"  Kaygı,\n\ngelecekteki bir tehdide — karşı bedenin alarmıdır.  ", 7},
		{"## Belirtiler\n\n- Çarpıntı\n- Uykusuzluk\n\n> Not: 3 hafta", 6},
		{"| a | b |\n|---|---|", 2},
	}
	for _, tt := range tests {
		if got := countWords(tt.text); got != tt.want {
			t.Errorf("countWords(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

func TestMarkdownNoiseIsNotCounted(t *testing.T) {
	body := "Bkz. [kaygı rehberi](https://example.com/a-very-long-path) ve ![grafik](img.png) önce<!-- not: kısalt -->sonra."
	if got := countWords(stripMarkdownNoise(body)); got != 6 {
		t.Fatalf("words = %d, want 6", got)
	}
}
