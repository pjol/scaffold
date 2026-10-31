// Package config loads runtime configuration from the environment (and
// backend/.env in development). Core settings live on Config; a feature reads
// its own settings in its New with the helpers below, so every variable the
// backend reads goes through String/Int/Bool/Duration/List/Required and
// `scaffold doctor` can find it. Document each one in scaffold.json.
package config

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Env         string // development | staging | production
	Port        string
	RunWorkers  bool
	CORSOrigins []string

	DatabaseURL string
	DBMaxConns  int32
	RedisURL    string // empty = in-memory realtime broker (single instance only)

	PublicAPIURL string // where this API is reachable
	PublicWebURL string // the web app's origin (links in email, redirects)

	ClerkSecretKey     string
	ClerkWebhookSecret string
	// AuthDevBypass accepts "Bearer dev:<subject>:<email>" tokens. Refused
	// outside development. dev-up turns it on so the stack runs without Clerk.
	AuthDevBypass bool
	AdminEmails   []string

	S3Endpoint       string
	S3Region         string
	S3Bucket         string
	S3AccessKeyID    string
	S3SecretKey      string
	S3PublicURL      string // public base URL for public objects
	S3ForcePathStyle bool
	LocalUploadDir   string // used when S3_BUCKET is empty (development only)

	MailFrom     string
	ResendAPIKey string // empty = mail is logged or written to MailSinkDir (development)
	MailSinkDir  string
	SupportEmail string

	TrustedProxyHops       int
	RateLimitPerMinuteIP   int
	RateLimitPerMinuteUser int
	RateLimitWSPerMinuteIP int
	RequestTimeout         time.Duration

	// Warnings are non-fatal notes; cmd/server logs them at boot.
	Warnings []string
}

func (c *Config) IsProduction() bool  { return c.Env == "production" }
func (c *Config) IsDevelopment() bool { return c.Env == "development" }

// WebOrigins are our browser origins (Clerk authorized parties).
func (c *Config) WebOrigins() []string {
	out := slices.Clone(c.CORSOrigins)
	if c.PublicWebURL != "" {
		out = append(out, c.PublicWebURL)
	}
	return out
}

// Load reads backend/.env (if present) then the environment. Real environment
// variables win over the file. Fatal on missing or unsafe values.
func Load() *Config {
	_ = godotenv.Load()

	c := &Config{
		Env:         String("APP_ENV", "development"),
		Port:        String("PORT", "8080"),
		RunWorkers:  Bool("RUN_WORKERS", true),
		CORSOrigins: List("CORS_ALLOWED_ORIGINS", ""),

		DatabaseURL: Required("DATABASE_URL"),
		DBMaxConns:  int32(Int("DB_MAX_CONNS", 20)),
		RedisURL:    String("REDIS_URL", ""),

		PublicAPIURL: String("PUBLIC_API_URL", "http://localhost:8080"),
		PublicWebURL: String("PUBLIC_WEB_URL", ""),

		ClerkSecretKey:     String("CLERK_SECRET_KEY", ""),
		ClerkWebhookSecret: String("CLERK_WEBHOOK_SECRET", ""),
		AuthDevBypass:      Bool("AUTH_DEV_BYPASS", false),
		AdminEmails:        lower(List("ADMIN_EMAILS", "")),

		S3Endpoint:       String("S3_ENDPOINT", ""),
		S3Region:         String("S3_REGION", "us-east-1"),
		S3Bucket:         String("S3_BUCKET", ""),
		S3AccessKeyID:    String("S3_ACCESS_KEY_ID", ""),
		S3SecretKey:      String("S3_SECRET_ACCESS_KEY", ""),
		S3PublicURL:      String("S3_PUBLIC_URL", ""),
		S3ForcePathStyle: Bool("S3_FORCE_PATH_STYLE", false),
		LocalUploadDir:   String("LOCAL_UPLOAD_DIR", "uploads"),

		MailFrom:     String("MAIL_FROM", "Scaffold App <no-reply@example.com>"),
		ResendAPIKey: String("RESEND_API_KEY", ""),
		MailSinkDir:  String("MAIL_SINK_DIR", ""),
		SupportEmail: String("SUPPORT_EMAIL", "support@example.com"),

		TrustedProxyHops:       Int("TRUSTED_PROXY_HOPS", 0),
		RateLimitPerMinuteIP:   Int("RATE_LIMIT_IP_PER_MIN", 600),
		RateLimitPerMinuteUser: Int("RATE_LIMIT_USER_PER_MIN", 300),
		RateLimitWSPerMinuteIP: Int("RATE_LIMIT_WS_PER_MIN", 60),
		RequestTimeout:         Duration("REQUEST_TIMEOUT", 20*time.Second),
	}
	warnings, err := c.Validate()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	c.Warnings = warnings
	return c
}

// Validate checks c for unsafe settings (fatal) and returns warnings.
func (c *Config) Validate() (warnings []string, err error) {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	switch c.Env {
	case "development", "staging", "production":
	default:
		fail("APP_ENV must be development, staging or production (got %q)", c.Env)
	}
	if c.AuthDevBypass && c.Env != "development" {
		fail("AUTH_DEV_BYPASS is only allowed when APP_ENV=development")
	}
	if c.ClerkSecretKey == "" && !c.AuthDevBypass {
		fail("CLERK_SECRET_KEY is required (or AUTH_DEV_BYPASS=true in development)")
	}
	if c.Env != "development" {
		for k, v := range map[string]string{
			"S3_BUCKET": c.S3Bucket, "PUBLIC_WEB_URL": c.PublicWebURL, "RESEND_API_KEY": c.ResendAPIKey,
			"CLERK_WEBHOOK_SECRET": c.ClerkWebhookSecret,
		} {
			if strings.TrimSpace(v) == "" {
				fail("%s is required outside development", k)
			}
		}
		if len(c.CORSOrigins) == 0 {
			fail("CORS_ALLOWED_ORIGINS is required outside development")
		}
	}
	if c.Env == "production" {
		if c.RedisURL == "" {
			warnings = append(warnings, "REDIS_URL unset: realtime only reaches clients on the same instance")
		}
		for _, o := range c.CORSOrigins {
			if o == "*" || IsLocalURL(o) {
				fail("CORS_ALLOWED_ORIGINS must list public web origins in production (got %q)", o)
			}
		}
		for k, v := range map[string]string{"PUBLIC_API_URL": c.PublicAPIURL, "PUBLIC_WEB_URL": c.PublicWebURL} {
			if IsLocalURL(v) || !strings.HasPrefix(v, "https://") {
				fail("%s must be a public https URL in production (got %q)", k, v)
			}
		}
	}
	if len(c.CORSOrigins) == 0 && c.Env == "development" {
		c.CORSOrigins = []string{"http://localhost:3000"}
		if c.PublicWebURL != "" {
			c.CORSOrigins = []string{c.PublicWebURL}
		}
	}
	errs = append(errs, problems()...)
	slices.Sort(warnings)
	return warnings, errors.Join(errs...)
}

// ---- helpers (use these for every variable, core or feature) ---------------

var (
	mu       sync.Mutex
	recorded []error
)

func record(err error) {
	mu.Lock()
	recorded = append(recorded, err)
	mu.Unlock()
}

func problems() []error {
	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(recorded)
}

// Check returns every problem recorded by Required/Int/Bool/Duration since
// boot. cmd/server calls it after all modules are constructed, so a feature's
// bad setting fails the boot with every other bad setting at once.
func Check() error { return errors.Join(problems()...) }

// Problem records an invalid setting a module detected (an unknown provider
// name, contradictory values). The boot fails at config.Check with every
// recorded problem. Never log.Fatal from a module.
func Problem(format string, args ...any) { record(fmt.Errorf(format, args...)) }

// String returns the variable, or def when unset or blank.
func String(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

// Required returns the variable and records a problem when it is unset.
func Required(k string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		record(fmt.Errorf("%s is required", k))
	}
	return v
}

// RequiredOutsideDev is Required except in development, where def is used.
func RequiredOutsideDev(k, def string) string {
	if String("APP_ENV", "development") == "development" {
		return String(k, def)
	}
	return Required(k)
}

func Int(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		record(fmt.Errorf("%s must be an integer (got %q)", k, v))
		return def
	}
	return n
}

func Bool(k string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		record(fmt.Errorf("%s must be true or false (got %q)", k, v))
		return def
	}
	return b
}

// Duration parses Go durations ("90s", "15m", "24h").
func Duration(k string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		record(fmt.Errorf("%s must be a duration like 30s or 15m (got %q)", k, v))
		return def
	}
	return d
}

// List splits a comma-separated variable, dropping blanks.
func List(k, def string) []string {
	out := []string{}
	for p := range strings.SplitSeq(String(k, def), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func lower(in []string) []string {
	for i := range in {
		in[i] = strings.ToLower(in[i])
	}
	return in
}

// IsLocalURL reports whether raw points at this machine (or is not a URL).
func IsLocalURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return true
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || h == "0.0.0.0" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}
