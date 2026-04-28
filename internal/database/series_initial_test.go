package database

import "testing"

func TestSeriesInitial(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		fallback string
		want     string
	}{
		{name: "chinese title", title: "进击的巨人", fallback: "folder", want: "J"},
		{name: "symbol prefixed chinese", title: "《火影忍者》", fallback: "folder", want: "H"},
		{name: "symbol prefixed english", title: "— One Piece", fallback: "folder", want: "O"},
		{name: "number prefixed chinese", title: "123-鬼灭之刃", fallback: "folder", want: "G"},
		{name: "fallback name", title: "", fallback: "【A版】Series", want: "A"},
		{name: "no letter", title: "12345...", fallback: "folder", want: "#"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SeriesInitial(tt.title, tt.fallback); got != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got)
			}
		})
	}
}
