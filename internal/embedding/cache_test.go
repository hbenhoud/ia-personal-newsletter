package embedding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// AC-4: Embedding cache

func TestCache_GetMiss(t *testing.T) {
	c, _ := NewCache(filepath.Join(t.TempDir(), "embeddings.json"))
	if v := c.Get("unknown text"); v != nil {
		t.Errorf("expected nil for cache miss, got %v", v)
	}
}

func TestCache_SetGet(t *testing.T) {
	c, _ := NewCache(filepath.Join(t.TempDir(), "embeddings.json"))
	vec := []float64{1.0, 2.0, 3.0}
	c.Set("hello world", vec)

	got := c.Get("hello world")
	if got == nil {
		t.Fatal("expected cached vector, got nil")
	}
	if len(got) != len(vec) {
		t.Fatalf("vector length mismatch: got %d, want %d", len(got), len(vec))
	}
	for i := range vec {
		if got[i] != vec[i] {
			t.Errorf("vector[%d]: got %f, want %f", i, got[i], vec[i])
		}
	}
}

func TestCache_Flush_Persist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embeddings.json")

	c1, _ := NewCache(path)
	c1.Set("test text", []float64{0.1, 0.2})
	if err := c1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Load from disk
	c2, err := NewCache(path)
	if err != nil {
		t.Fatalf("NewCache after flush: %v", err)
	}
	got := c2.Get("test text")
	if got == nil {
		t.Fatal("vector not persisted after flush")
	}
	if got[0] != 0.1 || got[1] != 0.2 {
		t.Errorf("persisted vector values wrong: %v", got)
	}
}

func TestCache_FlushProducesValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embeddings.json")

	c, _ := NewCache(path)
	c.Set("key", []float64{1, 2, 3})
	c.Flush() //nolint

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading cache file: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Errorf("cache file is not valid JSON: %v", err)
	}
}

func TestCache_NonExistentFile_Empty(t *testing.T) {
	c, err := NewCache(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("NewCache on non-existent file should not error: %v", err)
	}
	if c.Get("anything") != nil {
		t.Error("fresh cache should return nil for any key")
	}
}

func TestCache_CorruptFile_Recovered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	os.WriteFile(path, []byte("not json {{{"), 0644) //nolint

	c, err := NewCache(path)
	if err != nil {
		t.Fatalf("NewCache should recover from corrupt file: %v", err)
	}
	// Should start fresh
	if c.Get("anything") != nil {
		t.Error("recovered cache should be empty")
	}
}

func TestHashText_Consistent(t *testing.T) {
	h1 := hashText("hello")
	h2 := hashText("hello")
	if h1 != h2 {
		t.Errorf("hashText not deterministic: %q != %q", h1, h2)
	}
}

func TestHashText_Different(t *testing.T) {
	h1 := hashText("hello")
	h2 := hashText("world")
	if h1 == h2 {
		t.Error("different texts should produce different hashes")
	}
}

// AC-4: Profile recomputed only when content changes (hash comparison)
func TestCache_ProfileHashChanges(t *testing.T) {
	profile1 := "I like LLMs"
	profile2 := "I like RAG"

	h1 := hashText(profile1)
	h2 := hashText(profile2)

	if h1 == h2 {
		t.Error("different profile texts must produce different hashes")
	}

	// Same profile — same hash (no recompute needed)
	h3 := hashText(profile1)
	h4 := hashText(profile1)
	if h3 != h4 {
		t.Error("same profile text must produce same hash")
	}
}
