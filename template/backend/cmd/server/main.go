// Command server runs the API and (with RUN_WORKERS=true, the default) every
// module's background workers. Instances are identical; scale horizontally
// (set REDIS_URL so realtime reaches every instance).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"scaffoldapp/backend/internal/api"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/mail"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

func main() {
	bucketPolicy := flag.Bool("bucket-policy", false, "print the S3 bucket policy for this build's upload purposes and exit")
	flag.Parse()
	cfg := config.Load()
	if *bucketPolicy {
		printBucketPolicy(cfg)
		return
	}
	setupLogging(cfg)
	for _, w := range cfg.Warnings {
		slog.Warn("config: " + w)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, !cfg.IsDevelopment()); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	var broker realtime.Broker = realtime.NewMemory()
	if cfg.RedisURL != "" {
		if broker, err = realtime.NewRedis(ctx, cfg.RedisURL); err != nil {
			log.Fatalf("redis: %v", err)
		}
	}
	defer broker.Close()

	store, err := storage.New(ctx, cfg)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	var provider auth.Provider
	if cfg.ClerkSecretKey != "" {
		provider = auth.NewClerk(cfg.ClerkSecretKey, cfg.WebOrigins()...)
	}
	d := &app.Deps{
		Cfg:     cfg,
		Pool:    pool,
		Auth:    auth.New(pool, provider, cfg.AdminEmails, cfg.AuthDevBypass),
		Broker:  broker,
		Storage: store,
		Mail:    mail.New(cfg.ResendAPIKey, cfg.MailFrom, cfg.MailSinkDir),
		Notify:  notify.Realtime{Broker: broker},
	}

	built := api.Build(d, func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return errors.New("database unreachable")
		}
		if err := store.Ping(ctx); err != nil {
			return errors.New("storage unreachable")
		}
		return nil
	})
	if err := config.Check(); err != nil {
		log.Fatalf("config: %v", err)
	}
	built.Start(ctx, cfg.RunWorkers)

	names := make([]string, 0, len(built.Modules))
	for _, m := range built.Modules {
		names = append(names, m.Name())
	}
	slog.Info("starting", "env", cfg.Env, "port", cfg.Port, "modules", names,
		"auth", map[bool]string{true: "clerk", false: "dev-bypass only"}[provider != nil],
		"auth_dev_bypass", cfg.AuthDevBypass, "storage", map[bool]string{true: "s3", false: "local"}[cfg.S3Bucket != ""],
		"mail", mailMode(cfg),
		"realtime", map[bool]string{true: "redis", false: "memory"}[cfg.RedisURL != ""])

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           built.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	// Requests are done; let the work they started (push, email, fan-out)
	// finish before the broker and pool close.
	if err := bg.Wait(shutdownCtx); err != nil {
		slog.Warn("shutdown: background work cut off", "err", err)
	}
}

// printBucketPolicy builds the modules (they register their upload purposes
// in New) without a database or bucket, then prints the policy for
// S3_BUCKET: public purposes public-read, everything else private
// (docs/DEPLOYMENT.md). dev-up applies it to MinIO.
func printBucketPolicy(cfg *config.Config) {
	if cfg.S3Bucket == "" {
		fmt.Fprintln(os.Stderr, "-bucket-policy: set S3_BUCKET")
		os.Exit(2)
	}
	broker := realtime.NewMemory()
	api.Build(&app.Deps{
		Cfg: cfg, Broker: broker, Auth: auth.New(nil, nil, cfg.AdminEmails, false),
		Mail: mail.Log{}, Notify: notify.Realtime{Broker: broker},
	}, nil)
	fmt.Println(string(storage.BucketPolicy(cfg.S3Bucket)))
}

// setupLogging writes JSON to stdout outside development (the host collects
// it) and readable text in development. Never log secrets, tokens or bodies.
func setupLogging(cfg *config.Config) {
	level := slog.LevelInfo
	if config.String("LOG_LEVEL", "info") == "debug" {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewJSONHandler(os.Stdout, opts)
	if cfg.IsDevelopment() {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(h))
}

func mailMode(cfg *config.Config) string {
	switch {
	case cfg.ResendAPIKey != "":
		return "resend"
	case cfg.MailSinkDir != "":
		return "sink:" + cfg.MailSinkDir
	}
	return "log"
}
