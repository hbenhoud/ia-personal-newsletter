package embedding

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type cacheEntry struct {
	Vector []float64 `json:"vector"`
}

// Cache persists embedding vectors keyed by SHA256 of the input text.
type Cache struct {
	mu      sync.RWMutex
	path    string
	entries map[string]cacheEntry
}

// NewCache loads an existing cache file or creates a new empty cache.
func NewCache(path string) (*Cache, error) {
	c := &Cache{
		path:    path,
		entries: make(map[string]cacheEntry),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, fmt.Errorf("reading embedding cache: %w", err)
	}

	if err := json.Unmarshal(data, &c.entries); err != nil {
		// Corrupt cache — start fresh
		c.entries = make(map[string]cacheEntry)
	}

	return c, nil
}

// Get returns a cached vector for text, or nil if not cached.
func (c *Cache) Get(text string) []float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if e, ok := c.entries[hashText(text)]; ok {
		return e.Vector
	}
	return nil
}

// Set stores a vector for the given text.
func (c *Cache) Set(text string, vector []float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[hashText(text)] = cacheEntry{Vector: vector}
}

// Flush writes the cache to disk.
func (c *Cache) Flush() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling embedding cache: %w", err)
	}

	return os.WriteFile(c.path, data, 0644)
}

func hashText(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h)
}
