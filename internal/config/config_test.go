package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AC-1: Profile parsing

func TestParseProfile_AllFields(t *testing.T) {
	content := `# User profile

I am interested in LLMs, RAG, and agents.

## Topics to avoid
crypto, business

## Preferences
language: fr
recency_days: 14
theme: dark
level: expert
`
	p := parseProfile(content)

	if p.Interests != "I am interested in LLMs, RAG, and agents." {
		t.Errorf("unexpected Interests: %q", p.Interests)
	}
	if p.Avoid != "crypto, business" {
		t.Errorf("unexpected Avoid: %q", p.Avoid)
	}
	if p.Language != "fr" {
		t.Errorf("unexpected Language: %q", p.Language)
	}
	if p.RecencyDays != 14 {
		t.Errorf("unexpected RecencyDays: %d", p.RecencyDays)
	}
	if p.Theme != "dark" {
		t.Errorf("unexpected Theme: %q", p.Theme)
	}
	if p.Level != "expert" {
		t.Errorf("unexpected Level: %q", p.Level)
	}
	if p.Text != content {
		t.Error("Text should be the full raw content")
	}
}

func TestParseProfile_Defaults(t *testing.T) {
	// Minimal profile with no Preferences section
	content := "# User profile\n\nJust interests.\n"
	p := parseProfile(content)

	if p.Language != "en" {
		t.Errorf("default Language should be 'en', got %q", p.Language)
	}
	if p.RecencyDays != 7 {
		t.Errorf("default RecencyDays should be 7, got %d", p.RecencyDays)
	}
	if p.Theme != "minimal" {
		t.Errorf("default Theme should be 'minimal', got %q", p.Theme)
	}
	if p.Level != "intermediate" {
		t.Errorf("default Level should be 'intermediate', got %q", p.Level)
	}
}

func TestParseProfile_NoAvoidSection(t *testing.T) {
	content := "# User profile\n\nInterests only.\n\n## Preferences\nlanguage: en\nrecency_days: 7\ntheme: minimal\nlevel: beginner\n"
	p := parseProfile(content)
	if p.Avoid != "" {
		t.Errorf("Avoid should be empty, got %q", p.Avoid)
	}
}

// AC-1: WriteProfile + LoadProfile roundtrip

func TestWriteProfileLoadProfile_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.md")

	original := &Profile{
		Interests:   "LLMs and RAG",
		Level:       "expert",
		Avoid:       "crypto",
		Language:    "en",
		RecencyDays: 14,
		Theme:       "dark",
	}

	if err := WriteProfile(path, original); err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}

	loaded, err := LoadProfile(path)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}

	if loaded.Interests != original.Interests {
		t.Errorf("Interests mismatch: got %q, want %q", loaded.Interests, original.Interests)
	}
	if loaded.Level != original.Level {
		t.Errorf("Level mismatch: got %q, want %q", loaded.Level, original.Level)
	}
	if loaded.Avoid != original.Avoid {
		t.Errorf("Avoid mismatch: got %q, want %q", loaded.Avoid, original.Avoid)
	}
	if loaded.Language != original.Language {
		t.Errorf("Language mismatch: got %q, want %q", loaded.Language, original.Language)
	}
	if loaded.RecencyDays != original.RecencyDays {
		t.Errorf("RecencyDays mismatch: got %d, want %d", loaded.RecencyDays, original.RecencyDays)
	}
	if loaded.Theme != original.Theme {
		t.Errorf("Theme mismatch: got %q, want %q", loaded.Theme, original.Theme)
	}
}

// AC-1: Missing profile.md produces a clear error

func TestLoadProfile_MissingFile(t *testing.T) {
	_, err := LoadProfile("/nonexistent/path/profile.md")
	if err == nil {
		t.Fatal("expected error for missing profile.md, got nil")
	}
}

// AC-1: RunProfileWizard — all 6 questions produce a correct Profile

func TestRunProfileWizard_AllAnswers(t *testing.T) {
	input := strings.Join([]string{
		"LLMs, RAG, agents",  // interests
		"expert",             // level
		"crypto, business",   // avoid
		"fr",                 // language
		"2",                  // time window: 14 days
		"2",                  // theme: dark
		"",                   // EOF
	}, "\n")

	r := bufio.NewReader(strings.NewReader(input))
	p, err := RunProfileWizard(r)
	if err != nil {
		t.Fatalf("RunProfileWizard: %v", err)
	}

	if p.Interests != "LLMs, RAG, agents" {
		t.Errorf("Interests: got %q", p.Interests)
	}
	if p.Level != "expert" {
		t.Errorf("Level: got %q", p.Level)
	}
	if p.Avoid != "crypto, business" {
		t.Errorf("Avoid: got %q", p.Avoid)
	}
	if p.Language != "fr" {
		t.Errorf("Language: got %q", p.Language)
	}
	if p.RecencyDays != 14 {
		t.Errorf("RecencyDays: got %d", p.RecencyDays)
	}
	if p.Theme != "dark" {
		t.Errorf("Theme: got %q", p.Theme)
	}
}

func TestRunProfileWizard_Defaults(t *testing.T) {
	// Empty answers → all defaults
	input := "\n\n\n\n1\n1\n"
	r := bufio.NewReader(strings.NewReader(input))
	p, err := RunProfileWizard(r)
	if err != nil {
		t.Fatalf("RunProfileWizard: %v", err)
	}
	if p.Level != "intermediate" {
		t.Errorf("default Level: got %q", p.Level)
	}
	if p.Language != "en" {
		t.Errorf("default Language: got %q", p.Language)
	}
	if p.RecencyDays != 7 {
		t.Errorf("default RecencyDays: got %d", p.RecencyDays)
	}
	if p.Theme != "minimal" {
		t.Errorf("default Theme: got %q", p.Theme)
	}
}

func TestRunProfileWizard_CustomDays(t *testing.T) {
	input := "interests\nintermediate\n\nen\n4\n21\n1\n"
	r := bufio.NewReader(strings.NewReader(input))
	p, err := RunProfileWizard(r)
	if err != nil {
		t.Fatalf("RunProfileWizard: %v", err)
	}
	if p.RecencyDays != 21 {
		t.Errorf("custom RecencyDays: got %d, want 21", p.RecencyDays)
	}
}

// Multi-profile: Slugify

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"technical":     "technical",
		"AI & Business": "ai-business",
		"  Tech News  ": "tech-news",
		"a/b\\c":        "a-b-c",
		"":              "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// Multi-profile: Load with an explicit profiles list

func TestLoad_MultipleProfiles(t *testing.T) {
	dir := t.TempDir()
	writeProfileFile(t, filepath.Join(dir, "profiles", "technical.md"), "dark")
	writeProfileFile(t, filepath.Join(dir, "profiles", "business.md"), "paper")

	yaml := `
profiles:
  - name: technical
    profile: profiles/technical.md
    sources:
      rss:
        - https://example.com/tech.xml
  - name: business
    profile: profiles/business.md
    sources:
      rss:
        - https://example.com/biz1.xml
        - https://example.com/biz2.xml
`
	if err := os.WriteFile(filepath.Join(dir, "newsletter.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(cfg.Profiles))
	}
	tech := cfg.Profiles[0]
	if tech.Name != "technical" || tech.Slug() != "technical" {
		t.Errorf("unexpected technical profile: %+v", tech)
	}
	if tech.Profile.Theme != "dark" {
		t.Errorf("technical theme: got %q, want dark", tech.Profile.Theme)
	}
	if len(tech.Sources.RSS) != 1 {
		t.Errorf("technical feeds: got %d, want 1", len(tech.Sources.RSS))
	}
	if len(cfg.Profiles[1].Sources.RSS) != 2 {
		t.Errorf("business feeds: got %d, want 2", len(cfg.Profiles[1].Sources.RSS))
	}
}

// Multi-profile: backward-compat fallback (top-level sources → "default" profile)

func TestLoad_BackwardCompatFallback(t *testing.T) {
	dir := t.TempDir()
	writeProfileFile(t, filepath.Join(dir, "profile.md"), "minimal")

	yaml := `
sources:
  rss:
    - https://example.com/feed.xml
`
	if err := os.WriteFile(filepath.Join(dir, "newsletter.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Profiles) != 1 {
		t.Fatalf("expected 1 fallback profile, got %d", len(cfg.Profiles))
	}
	if cfg.Profiles[0].Name != "default" {
		t.Errorf("fallback profile name: got %q, want default", cfg.Profiles[0].Name)
	}
	if len(cfg.Profiles[0].Sources.RSS) != 1 {
		t.Errorf("fallback feeds: got %d, want 1", len(cfg.Profiles[0].Sources.RSS))
	}
}

func writeProfileFile(t *testing.T, path, theme string) {
	t.Helper()
	p := &Profile{Interests: "test interests", Level: "expert", Language: "fr", RecencyDays: 3, Theme: theme}
	if err := WriteProfile(path, p); err != nil {
		t.Fatalf("WriteProfile(%s): %v", path, err)
	}
}

// AC-1: WriteProfile creates parent directories

func TestWriteProfile_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "profile.md")

	p := &Profile{Interests: "test", Level: "expert", Language: "en", RecencyDays: 7, Theme: "minimal"}
	if err := WriteProfile(path, p); err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("profile.md not created: %v", err)
	}
}
