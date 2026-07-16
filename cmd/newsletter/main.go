package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pterm/pterm"
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
	var profileName string
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Run the full pipeline and generate this week's newsletter(s)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdGenerate(cmd.Context(), profileName)
		},
	}
	cmd.Flags().StringVar(&profileName, "profile", "", "generate only the named profile (default: all configured profiles)")
	return cmd
}

// --- profile ---

func profileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage your user profile",
	}
	cmd.AddCommand(
		profileListCmd(),
		profileSetupCmd(),
		profileShowCmd(),
		profileEditCmd(),
	)
	return cmd
}

func profileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdProfileList()
		},
	}
}

func profileSetupCmd() *cobra.Command {
	var profileName string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive wizard to create or overwrite a profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdProfileSetup(profileName)
		},
	}
	cmd.Flags().StringVar(&profileName, "profile", "", "profile name; writes config/profiles/<slug>.md (default: config/profile.md)")
	return cmd
}

func profileShowCmd() *cobra.Command {
	var profileName string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print a profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdProfileShow(profileName)
		},
	}
	cmd.Flags().StringVar(&profileName, "profile", "", "profile name (default: the sole profile)")
	return cmd
}

func profileEditCmd() *cobra.Command {
	var profileName string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a profile interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdProfileEdit(profileName)
		},
	}
	cmd.Flags().StringVar(&profileName, "profile", "", "profile name (default: the sole profile)")
	return cmd
}

// --- sources ---

func sourcesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sources",
		Short: "Manage RSS sources",
	}
	var profileName string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured RSS feeds",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdSourcesList(profileName)
		},
	}
	listCmd.Flags().StringVar(&profileName, "profile", "", "show only the named profile's feeds (default: all)")
	cmd.AddCommand(listCmd)
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

// --- business logic ---

func cmdGenerate(ctx context.Context, profileName string) error {
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}

	profiles, err := selectProfiles(cfg, profileName)
	if err != nil {
		return err
	}

	pterm.DefaultHeader.WithFullWidth().Println("AI Newsletter Generator")

	// Build shared providers once — the embedding cache is content-hashed, so
	// every profile safely reuses it.
	spinner, _ := pterm.DefaultSpinner.Start("Loading cache and providers...")
	embedder, cache, err := embedding.NewEmbedder(
		cfg.Embedding.Provider,
		cfg.Embedding.Model,
		cfg.Embedding.APIKeyEnv,
		cfg.Embedding.CachePath,
	)
	if err != nil {
		spinner.Fail("embedding provider: " + err.Error())
		return fmt.Errorf("embedding provider: %w", err)
	}
	defer func() {
		if flushErr := cache.Flush(); flushErr != nil {
			pterm.Warning.Printfln("failed to flush embedding cache: %v", flushErr)
		}
	}()
	llmProvider, err := llm.NewProvider(cfg.LLM.Provider, cfg.LLM.Model, cfg.LLM.APIKeyEnv)
	if err != nil {
		spinner.Fail("LLM provider: " + err.Error())
		return fmt.Errorf("LLM provider: %w", err)
	}
	spinner.Success("Cache and providers ready")

	promptContent, err := os.ReadFile("templates/prompts/summarize.tmpl")
	if err != nil {
		return fmt.Errorf("reading prompt template: %w", err)
	}

	// Generate one newsletter per selected profile. A failure on one profile
	// does not abort the others.
	var failures int
	for _, pc := range profiles {
		pterm.DefaultSection.Printfln("Profile: %s (theme %s, %s)", pc.Name, pc.Profile.Theme, pc.Profile.Language)
		outPath, genErr := generateProfile(ctx, cfg, pc, embedder, llmProvider, string(promptContent))
		if genErr != nil {
			failures++
			pterm.Error.Printfln("profile %q: %v", pc.Name, genErr)
			continue
		}
		if outPath != "" {
			if absPath, absErr := filepath.Abs(outPath); absErr == nil {
				pterm.Success.Println("file://" + absPath)
			} else {
				pterm.Success.Println(outPath)
			}
		}
	}

	// Rebuild the root landing page from every configured profile (including
	// ones not generated this run but with issues on disk).
	if err := writeLandingPage(cfg); err != nil {
		pterm.Warning.Printfln("failed to write landing page: %v", err)
	} else if absPath, absErr := filepath.Abs(filepath.Join(cfg.Output.SiteDir, "index.html")); absErr == nil {
		pterm.Println()
		pterm.DefaultBox.Println("file://" + absPath)
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d profiles failed to generate", failures, len(profiles))
	}
	return nil
}

// generateProfile runs the full pipeline for one profile and writes its issue
// and per-profile index under output/<slug>/. Returns the issue path, or an
// empty path when no articles matched.
func generateProfile(ctx context.Context, cfg *config.Config, pc config.ProfileConfig, embedder embedding.Embedder, llmProvider llm.Provider, promptContent string) (string, error) {
	prof := pc.Profile

	// Fetch RSS
	spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Fetching %d RSS feeds...", len(pc.Sources.RSS)))
	articles, err := ingestion.FetchAll(ctx, pc.Sources.RSS)
	if err != nil {
		spinner.Fail("ingestion: " + err.Error())
		return "", fmt.Errorf("ingestion: %w", err)
	}
	spinner.Success(fmt.Sprintf("Fetched %d articles", len(articles)))

	// Filter
	spinner, _ = pterm.DefaultSpinner.Start("Filtering by semantic similarity...")
	filterCfg := filtering.Config{
		Mode:                cfg.Filtering.Mode,
		SimilarityThreshold: cfg.Filtering.SimilarityThreshold,
		RecencyDays:         prof.RecencyDays,
		MaxArticles:         cfg.Filtering.MaxArticles,
		ExcludeKeywords:     cfg.Filtering.ExcludeKeywords,
	}
	scored, err := filtering.Filter(ctx, articles, prof.Text, embedder, filterCfg)
	if err != nil {
		spinner.Fail("filtering: " + err.Error())
		return "", fmt.Errorf("filtering: %w", err)
	}
	spinner.Success(fmt.Sprintf("%d relevant · %d discarded", len(scored), len(articles)-len(scored)))

	if len(scored) == 0 {
		pterm.Warning.Println("No articles matched this profile. Try lowering similarity_threshold in newsletter.yaml.")
		return "", nil
	}

	if cfg.Output.ItemsPerIssue > 0 && len(scored) > cfg.Output.ItemsPerIssue {
		scored = scored[:cfg.Output.ItemsPerIssue]
	}

	summarizer, err := generation.NewSummarizer(llmProvider, promptContent)
	if err != nil {
		return "", fmt.Errorf("creating summarizer: %w", err)
	}

	// Summarize with progress bar
	pterm.DefaultSection.Println("Generating Summaries")
	bar, _ := pterm.DefaultProgressbar.WithTotal(len(scored)).WithTitle("Summarizing").Start()
	summarizer.OnProgress = func(done, _ int) { bar.Increment() }
	summaries, err := summarizer.Summarize(ctx, scored, prof.Level, prof.Interests, prof.Language)
	if _, stopErr := bar.Stop(); stopErr != nil {
		pterm.Warning.Printfln("progress bar stop: %v", stopErr)
	}
	if err != nil {
		return "", fmt.Errorf("summarization: %w", err)
	}

	// Render into output/<slug>/
	spinner, _ = pterm.DefaultSpinner.Start("Rendering newsletter...")
	cssContent, err := loadThemeCSS(prof.Theme)
	if err != nil {
		spinner.Fail(err.Error())
		return "", err
	}
	newsletterTmpl, err := os.ReadFile("templates/newsletter.html")
	if err != nil {
		spinner.Fail(err.Error())
		return "", fmt.Errorf("reading newsletter template: %w", err)
	}
	indexTmpl, err := os.ReadFile("templates/index.html")
	if err != nil {
		spinner.Fail(err.Error())
		return "", fmt.Errorf("reading index template: %w", err)
	}
	siteDir := filepath.Join(cfg.Output.SiteDir, pc.Slug())
	gen, err := site.New(siteDir, string(newsletterTmpl), string(indexTmpl), cssContent, prof.Theme)
	if err != nil {
		spinner.Fail(err.Error())
		return "", fmt.Errorf("site generator: %w", err)
	}
	outPath, err := gen.WriteIssue(summaries, len(articles), prof.Language, prof.RecencyDays)
	if err != nil {
		spinner.Fail(err.Error())
		return "", fmt.Errorf("writing issue: %w", err)
	}
	if err := gen.WriteIndex(); err != nil {
		pterm.Warning.Printfln("failed to update index: %v", err)
	}
	spinner.Success(fmt.Sprintf("%d articles · theme: %s", len(summaries), prof.Theme))
	return outPath, nil
}

// writeLandingPage rebuilds output/index.html listing every configured profile.
func writeLandingPage(cfg *config.Config) error {
	homeTmpl, err := os.ReadFile("templates/home.html")
	if err != nil {
		return fmt.Errorf("reading home template: %w", err)
	}
	// The landing page adopts the first profile's theme so the whole site
	// feels cohesive (loadThemeCSS falls back to minimal for unknown themes).
	landingTheme := "minimal"
	if len(cfg.Profiles) > 0 {
		landingTheme = cfg.Profiles[0].Profile.Theme
	}
	homeCSS, err := loadThemeCSS(landingTheme)
	if err != nil {
		return err
	}
	inputs := make([]site.HomeProfileInput, 0, len(cfg.Profiles))
	for _, pc := range cfg.Profiles {
		inputs = append(inputs, site.HomeProfileInput{
			Name:     pc.Name,
			Slug:     pc.Slug(),
			Title:    profileTitle(pc),
			Language: pc.Profile.Language,
		})
	}
	return site.WriteHome(cfg.Output.SiteDir, inputs, string(homeTmpl), homeCSS)
}

// selectProfiles returns all profiles, or the single one matching name (by name or slug).
func selectProfiles(cfg *config.Config, name string) ([]config.ProfileConfig, error) {
	if name == "" {
		return cfg.Profiles, nil
	}
	for _, p := range cfg.Profiles {
		if p.Name == name || p.Slug() == name {
			return []config.ProfileConfig{p}, nil
		}
	}
	return nil, fmt.Errorf("unknown profile %q (configured: %s)", name, profileNames(cfg))
}

// findProfile resolves a single profile by name, or the sole profile if name is empty.
func findProfile(cfg *config.Config, name string) (*config.ProfileConfig, error) {
	if name == "" {
		if len(cfg.Profiles) == 1 {
			return &cfg.Profiles[0], nil
		}
		return nil, fmt.Errorf("multiple profiles configured (%s); specify --profile", profileNames(cfg))
	}
	for i := range cfg.Profiles {
		if cfg.Profiles[i].Name == name || cfg.Profiles[i].Slug() == name {
			return &cfg.Profiles[i], nil
		}
	}
	return nil, fmt.Errorf("unknown profile %q (configured: %s)", name, profileNames(cfg))
}

func profileNames(cfg *config.Config) string {
	names := make([]string, len(cfg.Profiles))
	for i, p := range cfg.Profiles {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}

// profileTitle derives a display title from the profile name (e.g. "technical" → "Technical").
func profileTitle(pc config.ProfileConfig) string {
	words := strings.Fields(strings.ReplaceAll(pc.Name, "-", " "))
	if len(words) == 0 {
		return pc.Name
	}
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func cmdProfileList() error {
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}
	fmt.Printf("Configured profiles (%d):\n", len(cfg.Profiles))
	for _, pc := range cfg.Profiles {
		fmt.Printf("  • %-12s theme=%-8s lang=%-3s level=%-12s feeds=%d\n",
			pc.Name, pc.Profile.Theme, pc.Profile.Language, pc.Profile.Level, len(pc.Sources.RSS))
	}
	return nil
}

func cmdProfileSetup(profileName string) error {
	var profilePath string
	if profileName == "" {
		profilePath = filepath.Join(configDir, "profile.md")
	} else {
		profilePath = filepath.Join(configDir, "profiles", config.Slugify(profileName)+".md")
	}

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
	if profileName != "" {
		fmt.Printf("Add a matching entry under 'profiles:' in config/newsletter.yaml (with its RSS sources) to include it in generation.\n")
	}
	return nil
}

func cmdProfileShow(profileName string) error {
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}
	pc, err := findProfile(cfg, profileName)
	if err != nil {
		return err
	}
	fmt.Println(pc.Profile.Text)
	return nil
}

func cmdProfileEdit(profileName string) error {
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}
	pc, err := findProfile(cfg, profileName)
	if err != nil {
		return err
	}
	profilePath := config.ProfileFilePath(configDir, pc)

	r := bufio.NewReader(os.Stdin)
	updated, err := config.RunProfileEditWizard(r, &pc.Profile)
	if err != nil {
		return err
	}
	if err := config.WriteProfile(profilePath, updated); err != nil {
		return fmt.Errorf("writing profile: %w", err)
	}
	fmt.Printf("\nProfile saved to %s\n", profilePath)
	return nil
}

func cmdSourcesList(profileName string) error {
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}
	profiles := cfg.Profiles
	if profileName != "" {
		pc, err := findProfile(cfg, profileName)
		if err != nil {
			return err
		}
		profiles = []config.ProfileConfig{*pc}
	}
	for _, pc := range profiles {
		fmt.Printf("[%s] RSS feeds (%d):\n", pc.Name, len(pc.Sources.RSS))
		for i, feed := range pc.Sources.RSS {
			fmt.Printf("  %d. %s\n", i+1, feed)
		}
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
	validThemes := map[string]bool{"minimal": true, "dark": true, "paper": true, "terminal": true, "modern": true}
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
