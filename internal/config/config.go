package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the full application configuration loaded from newsletter.yaml and profile.md.
type Config struct {
	Sources   SourcesConfig   `yaml:"sources"`
	Filtering FilteringConfig `yaml:"filtering"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	LLM       LLMConfig       `yaml:"llm"`
	Output    OutputConfig    `yaml:"output"`
	Profile   Profile
}

type SourcesConfig struct {
	RSS []string `yaml:"rss"`
}

type FilteringConfig struct {
	Mode                string   `yaml:"mode"`
	SimilarityThreshold float64  `yaml:"similarity_threshold"`
	MaxArticles         int      `yaml:"max_articles"`
	ExcludeKeywords     []string `yaml:"exclude_keywords"`
}

type EmbeddingConfig struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
	CachePath string `yaml:"cache_path"`
}

type LLMConfig struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type OutputConfig struct {
	SiteDir       string `yaml:"site_dir"`
	ItemsPerIssue int    `yaml:"items_per_issue"`
}

// Profile holds user preferences parsed from config/profile.md.
type Profile struct {
	Text         string // full profile text used for embedding
	Interests    string
	Level        string
	Avoid        string
	Language     string
	RecencyDays  int
	Theme        string
}

// Load reads newsletter.yaml and profile.md from the config directory.
func Load(configDir string) (*Config, error) {
	yamlPath := filepath.Join(configDir, "newsletter.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("reading newsletter.yaml: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing newsletter.yaml: %w", err)
	}

	// Apply defaults
	if cfg.Filtering.Mode == "" {
		cfg.Filtering.Mode = "semantic"
	}
	if cfg.Filtering.SimilarityThreshold == 0 {
		cfg.Filtering.SimilarityThreshold = 0.72
	}
	if cfg.Filtering.MaxArticles == 0 {
		cfg.Filtering.MaxArticles = 20
	}
	if cfg.Output.ItemsPerIssue == 0 {
		cfg.Output.ItemsPerIssue = 10
	}
	if cfg.Output.SiteDir == "" {
		cfg.Output.SiteDir = "./output"
	}
	if cfg.Embedding.CachePath == "" {
		cfg.Embedding.CachePath = "./.cache/embeddings.json"
	}

	profilePath := filepath.Join(configDir, "profile.md")
	profile, err := LoadProfile(profilePath)
	if err != nil {
		return nil, fmt.Errorf("reading profile.md: %w (run 'newsletter profile setup' first)", err)
	}
	cfg.Profile = *profile

	return &cfg, nil
}

// LoadProfile parses config/profile.md.
func LoadProfile(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseProfile(string(data)), nil
}

func parseProfile(content string) *Profile {
	p := &Profile{
		Text:        content,
		Level:       "intermediate",
		Language:    "en",
		RecencyDays: 7,
		Theme:       "minimal",
	}

	lines := strings.Split(content, "\n")
	var section string
	var bodyLines []string

	rePrefs := regexp.MustCompile(`^(\w+):\s*(.+)$`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if s, ok := strings.CutPrefix(trimmed, "## "); ok {
			section = s
			bodyLines = nil
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			section = ""
			continue
		}

		switch strings.ToLower(section) {
		case "topics to avoid":
			if trimmed != "" {
				p.Avoid = trimmed
			}
		case "preferences":
			if m := rePrefs.FindStringSubmatch(trimmed); m != nil {
				key, val := strings.ToLower(m[1]), m[2]
				switch key {
				case "language":
					p.Language = val
				case "recency_days":
					if n, err := strconv.Atoi(val); err == nil {
						p.RecencyDays = n
					}
				case "theme":
					p.Theme = val
				case "level":
					p.Level = val
				}
			}
		default:
			_ = bodyLines
		}
	}

	// Extract interests from the body (text before first ##)
	if before, _, ok := strings.Cut(content, "\n## "); ok {
		body := strings.TrimSpace(before)
		// Remove the H1 header line
		if _, after, ok2 := strings.Cut(body, "\n"); ok2 {
			body = strings.TrimSpace(after)
		}
		p.Interests = body
	}

	return p
}

// WriteProfile writes the profile.md file from a Profile struct.
func WriteProfile(path string, p *Profile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString("# User profile\n\n")
	sb.WriteString(p.Interests + "\n\n")
	if p.Avoid != "" {
		sb.WriteString("## Topics to avoid\n")
		sb.WriteString(p.Avoid + "\n\n")
	}
	sb.WriteString("## Preferences\n")
	fmt.Fprintf(&sb, "language: %s\n", p.Language)
	fmt.Fprintf(&sb, "recency_days: %d\n", p.RecencyDays)
	fmt.Fprintf(&sb, "theme: %s\n", p.Theme)
	fmt.Fprintf(&sb, "level: %s\n", p.Level)

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// RunProfileWizard runs an interactive stdin wizard and returns a populated Profile.
func RunProfileWizard(r *bufio.Reader) (*Profile, error) {
	p := &Profile{}

	fmt.Println("\n=== Newsletter Profile Setup ===")
	fmt.Println()

	fmt.Print("1. Describe your AI interests (e.g. LLMs, RAG, agents): ")
	interests, _ := r.ReadString('\n')
	p.Interests = strings.TrimSpace(interests)

	fmt.Print("2. Your expertise level [beginner/intermediate/expert] (default: intermediate): ")
	level, _ := r.ReadString('\n')
	level = strings.TrimSpace(strings.ToLower(level))
	if level == "" {
		level = "intermediate"
	}
	p.Level = level

	fmt.Print("3. Topics to avoid (e.g. crypto, business, fundraising): ")
	avoid, _ := r.ReadString('\n')
	p.Avoid = strings.TrimSpace(avoid)

	fmt.Print("4. Preferred newsletter language [en/fr] (default: en): ")
	lang, _ := r.ReadString('\n')
	lang = strings.TrimSpace(strings.ToLower(lang))
	if lang == "" {
		lang = "en"
	}
	p.Language = lang

	fmt.Println("5. Time window for articles:")
	fmt.Println("   [1] 7 days (default)")
	fmt.Println("   [2] 14 days")
	fmt.Println("   [3] 30 days")
	fmt.Println("   [4] Custom")
	fmt.Print("   Choice: ")
	windowChoice, _ := r.ReadString('\n')
	switch strings.TrimSpace(windowChoice) {
	case "2":
		p.RecencyDays = 14
	case "3":
		p.RecencyDays = 30
	case "4":
		fmt.Print("   Enter number of days: ")
		customDays, _ := r.ReadString('\n')
		if n, err := strconv.Atoi(strings.TrimSpace(customDays)); err == nil && n > 0 {
			p.RecencyDays = n
		} else {
			p.RecencyDays = 7
		}
	default:
		p.RecencyDays = 7
	}

	fmt.Println("6. Visual theme:")
	fmt.Println("   [1] minimal — white, clean (default)")
	fmt.Println("   [2] dark    — dark background, blue/purple accents")
	fmt.Println("   [3] paper   — cream background, serif, print style")
	fmt.Println("   [4] terminal — black background, monospace, green accents")
	fmt.Print("   Choice: ")
	themeChoice, _ := r.ReadString('\n')
	themes := map[string]string{"1": "minimal", "2": "dark", "3": "paper", "4": "terminal"}
	if t, ok := themes[strings.TrimSpace(themeChoice)]; ok {
		p.Theme = t
	} else {
		p.Theme = "minimal"
	}

	p.Text = buildProfileText(p)
	return p, nil
}

// RunProfileEditWizard re-runs the wizard with current values as defaults.
// Pressing Enter on any question keeps the existing value.
func RunProfileEditWizard(r *bufio.Reader, current *Profile) (*Profile, error) {
	p := &Profile{
		Interests:   current.Interests,
		Level:       current.Level,
		Avoid:       current.Avoid,
		Language:    current.Language,
		RecencyDays: current.RecencyDays,
		Theme:       current.Theme,
	}

	fmt.Println("\n=== Newsletter Profile Edit ===")
	fmt.Println("Press Enter to keep the current value.")
	fmt.Println()

	fmt.Printf("1. AI interests (current: %s)\n   New value: ", truncateDisplay(current.Interests, 60))
	if v := readOptional(r); v != "" {
		p.Interests = v
	}

	fmt.Printf("2. Expertise level (current: %s):\n", current.Level)
	fmt.Println("   [1] beginner  [2] intermediate  [3] expert")
	fmt.Print("   Choice (Enter to keep): ")
	if v := readOptional(r); v != "" {
		levels := map[string]string{"1": "beginner", "2": "intermediate", "3": "expert"}
		if l, ok := levels[v]; ok {
			p.Level = l
		} else {
			p.Level = strings.ToLower(v)
		}
	}

	fmt.Printf("3. Topics to avoid (current: %q)\n   New value (Enter to keep): ", current.Avoid)
	if v := readOptional(r); v != "" {
		p.Avoid = v
	}

	fmt.Printf("4. Newsletter language (current: %s):\n", current.Language)
	fmt.Println("   [1] en  [2] fr  or type any language code")
	fmt.Print("   Choice (Enter to keep): ")
	if v := readOptional(r); v != "" {
		langs := map[string]string{"1": "en", "2": "fr"}
		if l, ok := langs[v]; ok {
			p.Language = l
		} else {
			p.Language = strings.ToLower(v)
		}
	}

	fmt.Printf("5. Time window for articles (current: %d days):\n", current.RecencyDays)
	fmt.Println("   [1] 7 days  [2] 14 days  [3] 30 days  [4] Custom")
	fmt.Print("   Choice (Enter to keep): ")
	if v := readOptional(r); v != "" {
		switch v {
		case "1":
			p.RecencyDays = 7
		case "2":
			p.RecencyDays = 14
		case "3":
			p.RecencyDays = 30
		case "4":
			fmt.Print("   Enter number of days: ")
			if custom := readOptional(r); custom != "" {
				if n, err := strconv.Atoi(custom); err == nil && n > 0 {
					p.RecencyDays = n
				}
			}
		}
	}

	themeNames := map[string]string{"1": "minimal", "2": "dark", "3": "paper", "4": "terminal"}
	fmt.Printf("6. Visual theme (current: %s):\n", current.Theme)
	fmt.Println("   [1] minimal — white, clean")
	fmt.Println("   [2] dark    — dark background, blue accents")
	fmt.Println("   [3] paper   — cream background, serif")
	fmt.Println("   [4] terminal — black background, monospace, green")
	fmt.Print("   Choice (Enter to keep): ")
	if v := readOptional(r); v != "" {
		if t, ok := themeNames[v]; ok {
			p.Theme = t
		}
	}

	p.Text = buildProfileText(p)
	return p, nil
}

func readOptional(r *bufio.Reader) string {
	v, _ := r.ReadString('\n')
	return strings.TrimSpace(v)
}

func truncateDisplay(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func buildProfileText(p *Profile) string {
	var sb strings.Builder
	sb.WriteString("# User profile\n\n")
	sb.WriteString(p.Interests + "\n\n")
	if p.Avoid != "" {
		sb.WriteString("## Topics to avoid\n")
		sb.WriteString(p.Avoid + "\n\n")
	}
	sb.WriteString("## Preferences\n")
	fmt.Fprintf(&sb, "language: %s\n", p.Language)
	fmt.Fprintf(&sb, "recency_days: %d\n", p.RecencyDays)
	fmt.Fprintf(&sb, "theme: %s\n", p.Theme)
	fmt.Fprintf(&sb, "level: %s\n", p.Level)
	return sb.String()
}
