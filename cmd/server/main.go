// Command server runs the dynamic, SEO-first newsletter website. It renders
// pages server-side from Postgres (the source of truth populated by cmd/ingest).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/hbenhoud/ia-personal-newsletter/internal/dotenv"
	"github.com/hbenhoud/ia-personal-newsletter/internal/store"
	"github.com/hbenhoud/ia-personal-newsletter/internal/web"
)

func main() {
	dotenv.Load(".env")
	if err := run(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func run() error {
	ctx := context.Background()

	st, err := store.NewPostgres(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer st.Close()

	renderer, err := web.NewRenderer()
	if err != nil {
		return err
	}

	srv := web.NewServer(st, renderer, web.Config{
		SiteName:    envOr("SITE_NAME", "AI Newsletter"),
		BaseURL:     os.Getenv("SITE_BASE_URL"),
		Description: envOr("SITE_DESCRIPTION", "Curated AI news, summarized and ranked for you."),
	})

	addr := ":" + envOr("PORT", "8080")
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("server: listening on %s", addr)
	return httpSrv.ListenAndServe()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
