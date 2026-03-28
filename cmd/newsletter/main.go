package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hbenhoud/ia-personal-newsletter/internal/config"
	"github.com/hbenhoud/ia-personal-newsletter/internal/embedding"
	"github.com/hbenhoud/ia-personal-newsletter/internal/filtering"
	"github.com/hbenhoud/ia-personal-newsletter/internal/generation"
	"github.com/hbenhoud/ia-personal-newsletter/internal/ingestion"
	"github.com/hbenhoud/ia-personal-newsletter/internal/llm"
	"github.com/hbenhoud/ia-personal-newsletter/internal/site"
)

const configDir = "./config"

func main() {
	loadDotEnv(".env")
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// loadDotEnv reads a .env file and sets any unset environment variables from it.
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // .env is optional
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		// Only set if not already defined in the environment
		if os.Getenv(key) == "" {
			os.Setenv(key, value) //nolint:errcheck
		}
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "newsletter",
		Short: "Personal AI newsletter generator",
		Long:  "Fetch RSS feeds, filter by semantic similarity, summarize with an LLM, and render a static HTML newsletter.",
	}

	root.AddCommand(
		generateCmd(),
		profileCmd(),
		sourcesCmd(),
		llmCmd(),
		embeddingCmd(),
	)

	return root
}

// --- generate ---

func generateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "generate",
		Short: "Run the full pipeline and generate this week's newsletter",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdGenerate(cmd.Context())
		},
	}
}

// --- profile ---

func profileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage your user profile",
	}
	cmd.AddCommand(
		profileSetupCmd(),
		profileShowCmd(),
		profileEditCmd(),
	)
	return cmd
}

func profileSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive wizard to create or overwrite your profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdProfileSetup()
		},
	}
}

func profileShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the current profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdProfileShow()
		},
	}
}

func profileEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the profile in $EDITOR",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdProfileEdit()
		},
	}
}

// --- sources ---

func sourcesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sources",
		Short: "Manage RSS sources",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List configured RSS feeds",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdSourcesList()
		},
	})
	return cmd
}

// --- llm ---

func llmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llm",
		Short: "LLM provider utilities",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "test",
		Short: "Test LLM provider connectivity",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdLLMTest(cmd.Context())
		},
	})
	return cmd
}

// --- embedding ---

func embeddingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "embedding",
		Short: "Embedding provider utilities",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "test",
		Short: "Test embedding provider connectivity",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdEmbeddingTest(cmd.Context())
		},
	})
	return cmd
}

// --- business logic (unchanged) ---

func cmdGenerate(ctx context.Context) error {
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}

	fmt.Println("Loading embedding cache...")
	embedder, cache, err := embedding.NewEmbedder(
		cfg.Embedding.Provider,
		cfg.Embedding.Model,
		cfg.Embedding.APIKeyEnv,
		cfg.Embedding.CachePath,
	)
	if err != nil {
		return fmt.Errorf("embedding provider: %w", err)
	}
	defer func() {
		if flushErr := cache.Flush(); flushErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to flush embedding cache: %v\n", flushErr)
		}
	}()

	llmProvider, err := llm.NewProvider(cfg.LLM.Provider, cfg.LLM.Model, cfg.LLM.APIKeyEnv)
	if err != nil {
		return fmt.Errorf("LLM provider: %w", err)
	}

	fmt.Printf("Fetching %d RSS feeds...\n", len(cfg.Sources.RSS))
	articles, err := ingestion.FetchAll(ctx, cfg.Sources.RSS)
	if err != nil {
		return fmt.Errorf("ingestion: %w", err)
	}
	fmt.Printf("Fetched %d articles\n", len(articles))

	fmt.Println("Filtering by semantic similarity...")
	filterCfg := filtering.Config{
		Mode:                cfg.Filtering.Mode,
		SimilarityThreshold: cfg.Filtering.SimilarityThreshold,
		RecencyDays:         cfg.Profile.RecencyDays,
		MaxArticles:         cfg.Filtering.MaxArticles,
		ExcludeKeywords:     cfg.Filtering.ExcludeKeywords,
	}
	scored, err := filtering.Filter(ctx, articles, cfg.Profile.Text, embedder, filterCfg)
	if err != nil {
		return fmt.Errorf("filtering: %w", err)
	}
	fmt.Printf("Selected %d articles\n", len(scored))

	if len(scored) == 0 {
		fmt.Println("No articles matched your profile. Try lowering similarity_threshold in newsletter.yaml.")
		return nil
	}

	if cfg.Output.ItemsPerIssue > 0 && len(scored) > cfg.Output.ItemsPerIssue {
		scored = scored[:cfg.Output.ItemsPerIssue]
	}

	promptContent, err := os.ReadFile("templates/prompts/summarize.tmpl")
	if err != nil {
		return fmt.Errorf("reading prompt template: %w", err)
	}

	summarizer, err := generation.NewSummarizer(llmProvider, string(promptContent))
	if err != nil {
		return fmt.Errorf("creating summarizer: %w", err)
	}

	fmt.Println("Generating summaries...")
	summaries, err := summarizer.Summarize(ctx, scored, cfg.Profile.Level, cfg.Profile.Interests, cfg.Profile.Language)
	if err != nil {
		return fmt.Errorf("summarization: %w", err)
	}

	cssContent, err := loadThemeCSS(cfg.Profile.Theme)
	if err != nil {
		return err
	}
	newsletterTmpl, err := os.ReadFile("templates/newsletter.html")
	if err != nil {
		return fmt.Errorf("reading newsletter template: %w", err)
	}
	indexTmpl, err := os.ReadFile("templates/index.html")
	if err != nil {
		return fmt.Errorf("reading index template: %w", err)
	}

	gen, err := site.New(cfg.Output.SiteDir, string(newsletterTmpl), string(indexTmpl), cssContent, cfg.Profile.Theme)
	if err != nil {
		return fmt.Errorf("site generator: %w", err)
	}

	outPath, err := gen.WriteIssue(summaries, len(articles), cfg.Profile.Language)
	if err != nil {
		return fmt.Errorf("writing issue: %w", err)
	}

	if err := gen.WriteIndex(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to update index: %v\n", err)
	}

	fmt.Printf("\nNewsletter generated: %s\n", outPath)
	return nil
}

func cmdProfileSetup() error {
	profilePath := filepath.Join(configDir, "profile.md")

	if _, err := os.Stat(profilePath); err == nil {
		fmt.Print("A profile already exists. Overwrite? [y/N]: ")
		r := bufio.NewReader(os.Stdin)
		ans, _ := r.ReadString('\n')
		if ans != "y\n" && ans != "Y\n" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	r := bufio.NewReader(os.Stdin)
	profile, err := config.RunProfileWizard(r)
	if err != nil {
		return err
	}

	if err := config.WriteProfile(profilePath, profile); err != nil {
		return fmt.Errorf("writing profile: %w", err)
	}

	fmt.Printf("\nProfile saved to %s\n", profilePath)
	return nil
}

func cmdProfileShow() error {
	profilePath := filepath.Join(configDir, "profile.md")
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("reading profile: %w (run 'newsletter profile setup' first)", err)
	}
	fmt.Println(string(data))
	return nil
}

func cmdProfileEdit() error {
	profilePath := filepath.Join(configDir, "profile.md")
	cfg, err := config.Load(configDir)
	if err != nil {
		return fmt.Errorf("loading profile: %w (run 'newsletter profile setup' first)", err)
	}
	r := bufio.NewReader(os.Stdin)
	updated, err := config.RunProfileEditWizard(r, &cfg.Profile)
	if err != nil {
		return err
	}
	if err := config.WriteProfile(profilePath, updated); err != nil {
		return fmt.Errorf("writing profile: %w", err)
	}
	fmt.Printf("\nProfile saved to %s\n", profilePath)
	return nil
}

func cmdSourcesList() error {
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}
	fmt.Printf("Configured RSS feeds (%d):\n", len(cfg.Sources.RSS))
	for i, feed := range cfg.Sources.RSS {
		fmt.Printf("  %d. %s\n", i+1, feed)
	}
	return nil
}

func cmdLLMTest(ctx context.Context) error {
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}
	provider, err := llm.NewProvider(cfg.LLM.Provider, cfg.LLM.Model, cfg.LLM.APIKeyEnv)
	if err != nil {
		return err
	}
	fmt.Printf("Testing LLM provider: %s\n", provider.Name())
	reply, err := provider.Complete(ctx, "Say 'OK' and nothing else.", llm.GenerationConfig{MaxTokens: 10})
	if err != nil {
		return fmt.Errorf("LLM test failed: %w", err)
	}
	fmt.Printf("Response: %s\n", reply)
	fmt.Println("LLM connection OK")
	return nil
}

func cmdEmbeddingTest(ctx context.Context) error {
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}
	embedder, cache, err := embedding.NewEmbedder(
		cfg.Embedding.Provider,
		cfg.Embedding.Model,
		cfg.Embedding.APIKeyEnv,
		cfg.Embedding.CachePath,
	)
	if err != nil {
		return err
	}
	defer cache.Flush() //nolint:errcheck

	fmt.Printf("Testing embedder: %s/%s\n", cfg.Embedding.Provider, cfg.Embedding.Model)
	vec, err := embedder.Embed(ctx, "test embedding")
	if err != nil {
		return fmt.Errorf("embedding test failed: %w", err)
	}
	fmt.Printf("Vector dimensions: %d\n", len(vec))
	fmt.Println("Embedding connection OK")
	return nil
}

func loadThemeCSS(theme string) (string, error) {
	validThemes := map[string]bool{"minimal": true, "dark": true, "paper": true, "terminal": true}
	if !validThemes[theme] {
		fmt.Printf("warning: unknown theme %q, falling back to minimal\n", theme)
		theme = "minimal"
	}
	cssPath := filepath.Join("templates", "themes", theme+".css")
	data, err := os.ReadFile(cssPath)
	if err != nil {
		return "", fmt.Errorf("reading theme CSS: %w", err)
	}
	return string(data), nil
}
