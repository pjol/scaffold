// Command seed fills a development database: the dev identities (with the
// current legal versions accepted) and whatever each module's SeedDev adds.
// Refuses to run unless APP_ENV=development. Safe to re-run.
package main

import (
	"context"
	"log"
	"log/slog"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/api"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/legal"
	"scaffoldapp/backend/internal/mail"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

func main() {
	cfg := config.Load()
	if !cfg.IsDevelopment() {
		log.Fatal("seed: refusing to run outside APP_ENV=development")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL, 4)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool, false); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	store, err := storage.New(ctx, cfg)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	broker := realtime.NewMemory()
	d := &app.Deps{
		Cfg: cfg, Pool: pool, Broker: broker, Storage: store,
		Auth:   auth.New(pool, nil, cfg.AdminEmails, true),
		Mail:   mail.Log{},
		Notify: notify.Realtime{Broker: broker},
	}
	built := api.Build(d, nil)

	var users []uuid.UUID
	for _, id := range auth.DevIdentities {
		var uid uuid.UUID
		var role *string
		if id.AdminRole != "" {
			role = &id.AdminRole
		}
		err := pool.QueryRow(ctx, `
			INSERT INTO users (auth_subject, email, display_name, admin_role, terms_version_accepted, privacy_version_accepted)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (auth_subject) DO UPDATE SET
			  email = EXCLUDED.email, display_name = EXCLUDED.display_name, admin_role = EXCLUDED.admin_role,
			  terms_version_accepted = EXCLUDED.terms_version_accepted,
			  privacy_version_accepted = EXCLUDED.privacy_version_accepted, updated_at = now()
			RETURNING id`, id.Subject, id.Email, id.DisplayName, role, legal.TermsVersion, legal.PrivacyVersion,
		).Scan(&uid)
		if err != nil {
			log.Fatalf("seed user %s: %v", id.Email, err)
		}
		users = append(users, uid)
	}
	slog.Info("seeded dev identities", "count", len(users))

	for _, m := range built.Modules {
		if s, ok := m.(app.DevSeeder); ok {
			if err := s.SeedDev(ctx, users); err != nil {
				log.Fatalf("seed %s: %v", m.Name(), err)
			}
			slog.Info("seeded", "module", m.Name())
		}
	}
}
