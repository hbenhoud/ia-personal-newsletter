package store

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a title into a URL-safe slug, appending a short suffix derived
// from uniq (typically the article URL) so slugs stay stable and collision-free
// across editions.
func Slugify(title, uniq string) string {
	base := nonSlugChars.ReplaceAllString(strings.ToLower(title), "-")
	base = strings.Trim(base, "-")
	if len(base) > 70 {
		base = strings.Trim(base[:70], "-")
	}
	sum := sha256.Sum256([]byte(uniq))
	suffix := hex.EncodeToString(sum[:])[:8]
	if base == "" {
		return suffix
	}
	return base + "-" + suffix
}
