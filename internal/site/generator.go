package site

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
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
	RecencyDays  int
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

// WriteIssue generates output/YYYY-MM-DD/index.html for the given summaries.
func (g *Generator) WriteIssue(summaries []generation.Summary, totalFetched int, language string, recencyDays int) (string, error) {
	now := time.Now()
	issueDir := now.Format("2006-01-02")
	weekLabel := now.Format("2 Jan 2006")
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
		RecencyDays:  recencyDays,
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
		if !e.IsDir() || !isDateDir(e.Name()) {
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

// HomeProfileInput identifies one newsletter edition for the root landing page.
// The latest issue date and article count are read from the filesystem.
type HomeProfileInput struct {
	Name     string
	Slug     string
	Title    string
	Language string
}

// homeProfile is the rendered view of a profile on the landing page.
type homeProfile struct {
	Name         string
	Slug         string
	Title        string
	Language     string
	LatestLabel  string
	LatestPath   string // relative link to the latest issue, e.g. "technical/2026-07-16/index.html"
	IndexPath    string // relative link to the profile index, e.g. "technical/index.html"
	ArticleCount int
	IssueCount   int
}

type homeData struct {
	CSS         template.CSS
	Profiles    []homeProfile
	GeneratedAt string
}

// WriteHome renders the root landing page (rootDir/index.html) listing every
// newsletter edition. For each profile it scans rootDir/<slug> for the latest
// issue directory and its article count.
func WriteHome(rootDir string, profiles []HomeProfileInput, homeTmplContent, cssContent string) error {
	tmpl, err := template.New("home").Funcs(template.FuncMap{
		"not": func(v any) bool {
			if s, ok := v.([]homeProfile); ok {
				return len(s) == 0
			}
			return v == nil
		},
	}).Parse(homeTmplContent)
	if err != nil {
		return fmt.Errorf("parsing home template: %w", err)
	}

	var rendered []homeProfile
	for _, p := range profiles {
		hp := homeProfile{
			Name:      p.Name,
			Slug:      p.Slug,
			Title:     p.Title,
			Language:  p.Language,
			IndexPath: p.Slug + "/index.html",
		}
		profileDir := filepath.Join(rootDir, p.Slug)
		entries, err := os.ReadDir(profileDir)
		if err == nil {
			var dates []string
			for _, e := range entries {
				if e.IsDir() && isDateDir(e.Name()) {
					dates = append(dates, e.Name())
				}
			}
			sort.Sort(sort.Reverse(sort.StringSlice(dates)))
			hp.IssueCount = len(dates)
			if len(dates) > 0 {
				latest := dates[0]
				hp.LatestLabel = labelFromDir(latest)
				hp.LatestPath = p.Slug + "/" + latest + "/index.html"
				hp.ArticleCount = readArticleCount(filepath.Join(profileDir, latest, "meta.json"))
			}
		}
		rendered = append(rendered, hp)
	}

	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return fmt.Errorf("creating output root: %w", err)
	}
	outPath := filepath.Join(rootDir, "index.html")
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating home file: %w", err)
	}
	defer f.Close()

	return tmpl.Execute(f, homeData{
		CSS:         template.CSS(cssContent),
		Profiles:    rendered,
		GeneratedAt: time.Now().Format("2006-01-02 15:04"),
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

func isDateDir(name string) bool {
	_, err := time.Parse("2006-01-02", name)
	return err == nil
}

func labelFromDir(dir string) string {
	// "2026-03-29" → "29 Mar 2026"
	t, err := time.Parse("2006-01-02", dir)
	if err != nil {
		return dir
	}
	return t.Format("2 Jan 2006")
}
