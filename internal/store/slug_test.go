package store

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct {
		name   string
		title  string
		uniq   string
		prefix string // expected slug prefix (before the hash suffix)
	}{
		{"simple", "Hello World", "https://a/1", "hello-world-"},
		{"punctuation", "GPT-5: what's new?!", "https://a/2", "gpt-5-what-s-new-"},
		{"leading trailing", "  --Edge--  ", "https://a/3", "edge-"},
		{"empty title", "", "https://a/4", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slugify(tt.title, tt.uniq)
			if tt.prefix != "" && got[:len(tt.prefix)] != tt.prefix {
				t.Fatalf("Slugify(%q) = %q, want prefix %q", tt.title, got, tt.prefix)
			}
			if len(got) < 8 {
				t.Fatalf("Slugify(%q) = %q, want an 8-char hash suffix", tt.title, got)
			}
		})
	}
}

func TestSlugifyStableAndUnique(t *testing.T) {
	a := Slugify("Same Title", "https://a/1")
	b := Slugify("Same Title", "https://a/2")
	if a == b {
		t.Fatalf("distinct URLs produced identical slugs: %q", a)
	}
	if a != Slugify("Same Title", "https://a/1") {
		t.Fatal("Slugify is not deterministic for the same inputs")
	}
}

func TestVecToText(t *testing.T) {
	if got := vecToText(nil); got != nil {
		t.Fatalf("vecToText(nil) = %v, want nil", got)
	}
	if got := vecToText([]float32{}); got != nil {
		t.Fatalf("vecToText(empty) = %v, want nil", got)
	}
	got := vecToText([]float32{0.5, -1, 2})
	want := "[0.5,-1,2]"
	if got != want {
		t.Fatalf("vecToText = %v, want %q", got, want)
	}
}
