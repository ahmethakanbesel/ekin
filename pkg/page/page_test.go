package page_test

import (
	"errors"
	"testing"

	"ekin/pkg/page"
)

type key struct {
	At int64   `json:"a"`
	ID string  `json:"i"`
	S  float64 `json:"s"`
}

func TestCursorRoundTrip(t *testing.T) {
	want := key{At: 1760700000123, ID: "0192f0c4-7d3b-7a51-9f1e-2c0e5d4b9a10", S: 2457.6000000000004}
	var got key
	ok, err := page.Decode(page.Encode(want), &got)
	if err != nil || !ok || got != want {
		t.Fatalf("decode = %+v %v %v", got, ok, err)
	}
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name    string
		cursor  string
		wantOK  bool
		wantErr error
	}{
		{name: "empty starts at the beginning"},
		{name: "not base64", cursor: "%%%", wantErr: page.ErrInvalid},
		{name: "not json", cursor: "bm90LWpzb24", wantErr: page.ErrInvalid},
		{name: "valid", cursor: page.Encode(key{At: 1}), wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var k key
			ok, err := page.Decode(tt.cursor, &k)
			if ok != tt.wantOK || !errors.Is(err, tt.wantErr) {
				t.Fatalf("decode = %v, %v", ok, err)
			}
		})
	}
}

func TestLimit(t *testing.T) {
	tests := []struct{ n, want int }{{0, 50}, {-3, 50}, {1, 1}, {200, 200}, {201, 200}}
	for _, tt := range tests {
		if got := page.Limit(tt.n, 50, 200); got != tt.want {
			t.Errorf("Limit(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}
