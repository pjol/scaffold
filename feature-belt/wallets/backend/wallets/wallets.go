// Package wallets links EVM wallets to accounts. External wallets prove
// control with Sign-In with Ethereum (EIP-4361); embedded wallets register
// from a verified Privy identity token. Balances are read over JSON-RPC from
// the chains in WALLETS_CHAINS, and ERC-20 transfers arrive from an indexer
// webhook. Clerk stays the identity provider: a wallet is an add-on to the
// signed-in user. This module never holds private keys.
package wallets

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/realtime"
)

// Realtime events on the owner's user topic (user:<id>). Delivery is
// at-most-once; clients refetch on reconnect.
const (
	EventTransfer = "wallets.transfer" // data: Transfer (direction from the recipient's view)
	EventChanged  = "wallets.changed"  // data: {"wallet_id","action":"linked|updated|unlinked"}
)

// KindReceived is the notification kind for incoming transfers. When the
// notifications feature is installed, register it there (see FEATURE.md).
const KindReceived = "wallets.received"

type Service struct {
	d        *app.Deps
	cfg      settings
	registry *Registry
	privy    *privyVerifier // nil: embedded wallets are not configured
	rpc      *rpcClient
	balances *balanceCache
	now      func() time.Time
}

type settings struct {
	maxPerUser    int
	siweURI       string
	siweDomain    string
	indexerSecret string
	privyAppID    string
}

// challengeTTL is how long a SIWE challenge stays valid (single use).
const challengeTTL = 10 * time.Minute

func New(d *app.Deps) *Service {
	production := d.Cfg.IsProduction()
	// Bad settings are recorded with config.Problem: the boot fails at
	// config.Check with every problem at once, and New still returns a
	// usable (feature-off) service so construction order never matters.
	reg, err := ParseChains(config.String("WALLETS_CHAINS", ""), int64(config.Int("WALLETS_DEFAULT_CHAIN_ID", 0)), production)
	if err != nil {
		config.Problem("%v", err)
		reg = &Registry{byID: map[int64]*Chain{}}
	}
	s := &Service{
		d:        d,
		registry: reg,
		rpc:      newRPCClient(config.Duration("WALLETS_RPC_TIMEOUT", 5*time.Second)),
		balances: newBalanceCache(config.Duration("WALLETS_BALANCE_CACHE_TTL", 15*time.Second)),
		now:      time.Now,
		cfg: settings{
			maxPerUser:    config.Int("WALLETS_MAX_PER_USER", 10),
			indexerSecret: config.String("WALLETS_INDEXER_SECRET", ""),
		},
	}
	if s.cfg.maxPerUser < 1 {
		config.Problem("WALLETS_MAX_PER_USER must be at least 1 (got %d)", s.cfg.maxPerUser)
		s.cfg.maxPerUser = 1
	}

	// SIWE domain and URI: the origin users see in their wallet's prompt.
	raw := config.String("WALLETS_SIWE_URI", "")
	if raw == "" {
		raw = d.Cfg.PublicWebURL
	}
	if raw == "" && len(d.Cfg.CORSOrigins) > 0 && d.Cfg.CORSOrigins[0] != "*" {
		raw = d.Cfg.CORSOrigins[0]
	}
	if raw == "" {
		raw = d.Cfg.PublicAPIURL
	}
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	switch {
	case err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http"):
		config.Problem("WALLETS_SIWE_URI must be the web app's origin, e.g. https://app.example.com (got %q)", raw)
	case production && u.Scheme != "https":
		config.Problem("WALLETS_SIWE_URI must be https in production (got %q)", raw)
	default:
		s.cfg.siweURI, s.cfg.siweDomain = u.Scheme+"://"+u.Host, u.Host
	}

	appID := config.String("PRIVY_APP_ID", "")
	key := config.String("PRIVY_VERIFICATION_KEY", "")
	switch {
	case appID != "" && key != "":
		if s.privy, err = newPrivyVerifier(appID, key); err != nil {
			config.Problem("PRIVY_VERIFICATION_KEY: %v", err)
		} else {
			s.cfg.privyAppID = appID
		}
	case appID != "" || key != "":
		config.Problem("set both PRIVY_APP_ID and PRIVY_VERIFICATION_KEY to enable embedded wallets, or neither")
	}

	slog.Info("wallets configured", "chains", len(reg.Chains()), "embedded", s.privy != nil,
		"indexer", s.cfg.indexerSecret != "", "siwe_domain", s.cfg.siweDomain)
	return s
}

func (s *Service) Name() string { return "wallets" }

func (s *Service) MountPublic(r chi.Router) {
	r.Get("/wallets/config", httpx.Handler(s.clientConfig))
}

func (s *Service) MountUser(r chi.Router) {
	limited := r.With(auth.RequireActive, auth.RateLimit(20, time.Minute))
	r.Get("/wallets", httpx.Handler(s.list))
	limited.Post("/wallets/link/challenge", httpx.Handler(s.challenge))
	limited.Post("/wallets/link", httpx.Handler(s.link))
	limited.Post("/wallets/embedded", httpx.Handler(s.registerEmbedded))
	r.With(auth.RequireActive).Patch("/wallets/{id}", httpx.Handler(s.update))
	r.With(auth.RequireActive).Delete("/wallets/{id}", httpx.Handler(s.unlink))
	r.With(auth.RateLimit(30, time.Minute)).Get("/wallets/balances", httpx.Handler(s.getBalances))
	r.Get("/wallets/activity", httpx.Handler(s.activity))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/wallets", httpx.Handler(s.adminList))
	r.Delete("/wallets/{id}", httpx.Handler(s.adminUnlink))
}

func (s *Service) MountWebhooks(r chi.Router) {
	r.Post("/wallets/indexer", s.indexerWebhook)
}

func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "wallets.prune_challenges", time.Hour, s.pruneChallenges)
}

// pruneChallenges drops SIWE challenges a day after they expire.
func (s *Service) pruneChallenges(ctx context.Context) error {
	_, err := s.d.Pool.Exec(ctx, `DELETE FROM wallet_nonces WHERE expires_at < now() - interval '1 day'`)
	return err
}

func (s *Service) clientConfig(w http.ResponseWriter, _ *http.Request) error {
	cc := ClientConfig{Chains: s.registry.public(), MaxWallets: s.cfg.maxPerUser, LinkURL: s.cfg.siweURI + "/settings/wallets"}
	if id := s.registry.DefaultChainID(); id != 0 {
		cc.DefaultChainID = &id
	}
	if s.privy != nil {
		provider, appID := "privy", s.cfg.privyAppID
		cc.Embedded = EmbeddedConfig{Enabled: true, Provider: &provider, PrivyAppID: &appID}
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	httpx.OK(w, cc)
	return nil
}

// ---- DTOs --------------------------------------------------------------------

// Wallet is a linked address as its owner sees it.
type Wallet struct {
	ID        uuid.UUID `json:"id"`
	Address   string    `json:"address"` // EIP-55
	Kind      string    `json:"kind"`    // external | embedded
	Label     string    `json:"label"`
	IsPrimary bool      `json:"is_primary"`
	CreatedAt time.Time `json:"created_at"`
}

const walletCols = `id, address, kind, label, is_primary, created_at`

func scanWallets(rows pgx.Rows) ([]Wallet, error) {
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Wallet])
	for i := range list {
		list[i].Address = checksum(list[i].Address)
	}
	if list == nil {
		list = []Wallet{}
	}
	return list, err
}

// Balance is one asset balance of one wallet on one chain. Amount is in base
// units as a decimal string (format with Decimals on the client); it is null
// when the read failed, and Error says why.
type Balance struct {
	WalletID     uuid.UUID `json:"wallet_id"`
	Address      string    `json:"address"`
	ChainID      int64     `json:"chain_id"`
	TokenAddress *string   `json:"token_address"` // null: the chain's native coin
	Symbol       string    `json:"symbol"`
	Decimals     int       `json:"decimals"`
	Amount       *string   `json:"amount"`
	Error        *string   `json:"error"` // rpc_unavailable | invalid_response
}

func (b *Balance) cacheKey() string {
	token := "native"
	if b.TokenAddress != nil {
		token = strings.ToLower(*b.TokenAddress)
	}
	return strings.Join([]string{itoa(b.ChainID), strings.ToLower(b.Address), token}, ":")
}

// Transfer is an ERC-20 transfer touching the viewer's wallets. Amount is
// base units as a decimal string.
type Transfer struct {
	ID           uuid.UUID `json:"id"`
	ChainID      int64     `json:"chain_id"`
	TxHash       string    `json:"tx_hash"`
	LogIndex     int       `json:"log_index"`
	BlockNumber  int64     `json:"block_number"`
	TokenAddress string    `json:"token_address"`
	Symbol       string    `json:"symbol"`
	Decimals     int       `json:"decimals"`
	From         string    `json:"from"`
	To           string    `json:"to"`
	Amount       string    `json:"amount"`
	Direction    string    `json:"direction"` // in | out | self
	ExplorerURL  *string   `json:"explorer_url"`
	OccurredAt   time.Time `json:"occurred_at"`
}

// transferRow is a wallet_transfers row; view turns it into a Transfer for
// one user.
type transferRow struct {
	ID          uuid.UUID
	ChainID     int64
	TxHash      string
	LogIndex    int
	BlockNumber int64
	Token       string
	Symbol      string
	Decimals    int
	From        string
	To          string
	Amount      string
	FromUser    *uuid.UUID
	ToUser      *uuid.UUID
	OccurredAt  time.Time
}

const transferCols = `id, chain_id, tx_hash, log_index, block_number, token_address, token_symbol,
	token_decimals, from_address, to_address, amount::text, from_user_id, to_user_id, occurred_at`

func (s *Service) view(t transferRow, viewer uuid.UUID) Transfer {
	in := t.ToUser != nil && *t.ToUser == viewer
	out := t.FromUser != nil && *t.FromUser == viewer
	dir := "in"
	switch {
	case in && out:
		dir = "self"
	case out:
		dir = "out"
	}
	return Transfer{
		ID: t.ID, ChainID: t.ChainID, TxHash: t.TxHash, LogIndex: t.LogIndex, BlockNumber: t.BlockNumber,
		TokenAddress: checksum(t.Token), Symbol: t.Symbol, Decimals: t.Decimals,
		From: checksum(t.From), To: checksum(t.To), Amount: t.Amount, Direction: dir,
		ExplorerURL: s.registry.TxURL(t.ChainID, t.TxHash), OccurredAt: t.OccurredAt,
	}
}

// publish sends a realtime event to a user; best-effort.
func (s *Service) publish(ctx context.Context, userID uuid.UUID, event string, data any) {
	if err := s.d.Broker.Publish(ctx, realtime.UserTopic(userID), event, data); err != nil {
		slog.WarnContext(ctx, "wallets: realtime publish failed", "event", event, "err", err)
	}
}

func (s *Service) publishChanged(ctx context.Context, userID, walletID uuid.UUID, action string) {
	s.publish(ctx, userID, EventChanged, map[string]any{"wallet_id": walletID, "action": action})
}
