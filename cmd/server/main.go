// Command server runs the dynamic, SEO-first newsletter website. It renders
// pages server-side from Postgres (the source of truth populated by cmd/ingest).
package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/hbenhoud/ia-personal-newsletter/internal/dotenv"
	"github.com/hbenhoud/ia-personal-newsletter/internal/email"
	"github.com/hbenhoud/ia-personal-newsletter/internal/store"
	"github.com/hbenhoud/ia-personal-newsletter/internal/web"
)

func main() {
	dotenv.Load(".env")
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Sync() }()

	if err := run(logger); err != nil {
		logger.Fatal("server startup failed", zap.Error(err))
	}
}

func run(logger *zap.Logger) error {
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

	// Email is optional: without EMAIL_API_KEY the subscribe form degrades
	// gracefully to "launching soon".
	var sender email.Sender
	if cfg, ok := email.ConfigFromEnv(os.Getenv); ok {
		sender, err = email.NewSender(cfg)
		if err != nil {
			return err
		}
		logger.Info("email enabled", zap.String("provider", sender.Name()))
	} else {
		logger.Info("email not configured", zap.String("hint", "set EMAIL_API_KEY to enable subscriptions"))
	}

	srv := web.NewServer(st, renderer, web.Config{
		SiteName:        envOr("SITE_NAME", "AI Newsletter"),
		BaseURL:         os.Getenv("SITE_BASE_URL"),
		Description:     envOr("SITE_DESCRIPTION", "Curated AI news, summarized and ranked for you."),
		GAMeasurementID: os.Getenv("GA_MEASUREMENT_ID"),
	}, sender, logger)

	addr := ":" + envOr("PORT", "8080")
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("listening", zap.String("addr", addr))
	return httpSrv.ListenAndServe()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
