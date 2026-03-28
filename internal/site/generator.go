package site

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hbenhoud/ia-personal-newsletter/internal/generation"
)

// IssueInfo is a lightweight record used for the index page.
type IssueInfo struct {
	Label        string
	Path         string
	ArticleCount int
	GeneratedAt  string
}

type newsletterData struct {
	WeekLabel    string
	Language     string
	CSS          template.CSS
	Summaries    []generation.Summary
	ArticleCount int
	TotalFetched int
	GeneratedAt  string
}

type indexData struct {
	CSS    template.CSS
	Issues []IssueInfo
}

// Generator writes the static HTML site.
type Generator struct {
	siteDir          string
	newsletterTmpl   *template.Template
	indexTmpl        *template.Template
	themeName        string
	css              string
}

// New creates a Generator from template and theme content.
func New(siteDir, newsletterTmplContent, indexTmplContent, cssContent, themeName string) (*Generator, error) {
	// html/template auto-escapes; use template.HTML for CSS injection
	newsletterTmpl, err := template.New("newsletter").Parse(newsletterTmplContent)
	if err != nil {
		return nil, fmt.Errorf("parsing newsletter template: %w", err)
	}

	// index.html uses a custom "not" function
	funcMap := template.FuncMap{
		"not": func(v any) bool {
			if v == nil {
				return true
			}
			if s, ok := v.([]IssueInfo); ok {
				return len(s) == 0
			}
			return false
		},
	}
	indexTmpl, err := template.New("index").Funcs(funcMap).Parse(indexTmplContent)
	if err != nil {
		return nil, fmt.Errorf("parsing index template: %w", err)
	}

	return &Generator{
		siteDir:        siteDir,
		newsletterTmpl: newsletterTmpl,
		indexTmpl:      indexTmpl,
		themeName:      themeName,
		css:            cssContent,
	}, nil
}

// WriteIssue generates output/YYYY-WW/index.html for the given summaries.
func (g *Generator) WriteIssue(summaries []generation.Summary, totalFetched int, language string) (string, error) {
	now := time.Now()
	year, week := now.ISOWeek()
	weekLabel := fmt.Sprintf("Week %d, %d", week, year)
	issueDir := fmt.Sprintf("%04d-W%02d", year, week)
	outDir := filepath.Join(g.siteDir, issueDir)

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", fmt.Errorf("creating output dir: %w", err)
	}

	data := newsletterData{
		WeekLabel:    weekLabel,
		Language:     language,
		CSS:          template.CSS(g.css),
		Summaries:    summaries,
		ArticleCount: len(summaries),
		TotalFetched: totalFetched,
		GeneratedAt:  now.Format("2006-01-02 15:04"),
	}

	outPath := filepath.Join(outDir, "index.html")
	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("creating issue file: %w", err)
	}
	defer f.Close()

	if err := g.newsletterTmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("rendering newsletter: %w", err)
	}

	metaPath := filepath.Join(outDir, "meta.json")
	metaData, _ := json.Marshal(map[string]int{"articleCount": len(summaries)})
	_ = os.WriteFile(metaPath, metaData, 0644)

	return outPath, nil
}

// WriteIndex regenerates output/index.html by scanning existing issue directories.
func (g *Generator) WriteIndex() error {
	entries, err := os.ReadDir(g.siteDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var issues []IssueInfo
	for _, e := range entries {
		if !e.IsDir() || !strings.Contains(e.Name(), "-W") {
			continue
		}
		issueIndex := filepath.Join(g.siteDir, e.Name(), "index.html")
		info, err := os.Stat(issueIndex)
		if err != nil {
			continue
		}
		count := readArticleCount(filepath.Join(g.siteDir, e.Name(), "meta.json"))
		issues = append(issues, IssueInfo{
			Label:        labelFromDir(e.Name()),
			Path:         e.Name(),
			ArticleCount: count,
			GeneratedAt:  info.ModTime().Format("2006-01-02"),
		})
	}

	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Path > issues[j].Path // newest first
	})

	outPath := filepath.Join(g.siteDir, "index.html")
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating index file: %w", err)
	}
	defer f.Close()

	return g.indexTmpl.Execute(f, indexData{
		CSS:    template.CSS(g.css),
		Issues: issues,
	})
}

func readArticleCount(metaPath string) int {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return 0
	}
	var m struct {
		ArticleCount int `json:"articleCount"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return 0
	}
	return m.ArticleCount
}

func labelFromDir(dir string) string {
	// "2026-W13" → "Week 13, 2026"
	parts := strings.SplitN(dir, "-W", 2)
	if len(parts) == 2 {
		return fmt.Sprintf("Week %s, %s", parts[1], parts[0])
	}
	return dir
}
