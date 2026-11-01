package wallets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"scaffoldapp/backend/internal/config"
)

// Chain is one EVM network from WALLETS_CHAINS. RPCURL usually embeds a
// provider key, so it stays on the server; PublicRPCURL is an optional
// keyless endpoint clients may use.
type Chain struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	RPCURL         string  `json:"rpc_url"`
	PublicRPCURL   string  `json:"public_rpc_url"`
	ExplorerURL    string  `json:"explorer_url"`
	NativeSymbol   string  `json:"native_symbol"`
	NativeDecimals *int    `json:"native_decimals"`
	Testnet        bool    `json:"testnet"`
	Tokens         []Token `json:"tokens"`
}

// Token is an ERC-20 on a chain. Decimals is a count of digits after the
// point (6 for USDC, 18 for most tokens), never a multiplier like 1000000.
type Token struct {
	Address  string `json:"address"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals *int   `json:"decimals"`

	addr Address
}

// maxDecimals bounds decimals: numeric(78,0) holds 10^77, and no real token
// uses more than 36 places. Larger values are almost always a multiplier
// typed where a count belongs.
const maxDecimals = 36

// Registry is the parsed, validated chain configuration.
type Registry struct {
	chains    []*Chain
	byID      map[int64]*Chain
	defaultID int64
}

// ParseChains parses WALLETS_CHAINS (a JSON array; empty means no chains)
// and picks the default chain (defaultID, else the first chain).
func ParseChains(raw string, defaultID int64, production bool) (*Registry, error) {
	r := &Registry{byID: map[int64]*Chain{}}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if defaultID != 0 {
			return nil, errors.New("WALLETS_DEFAULT_CHAIN_ID is set but WALLETS_CHAINS is empty")
		}
		return r, nil
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r.chains); err != nil {
		return nil, fmt.Errorf("WALLETS_CHAINS must be a JSON array of chains: %w", err)
	}
	for i, c := range r.chains {
		where := fmt.Sprintf("WALLETS_CHAINS[%d]", i)
		if c.ID <= 0 {
			return nil, fmt.Errorf("%s: id must be the positive EIP-155 chain id", where)
		}
		where = fmt.Sprintf("WALLETS_CHAINS chain %d", c.ID)
		if _, dup := r.byID[c.ID]; dup {
			return nil, fmt.Errorf("%s is listed twice", where)
		}
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" {
			return nil, fmt.Errorf("%s: name is required", where)
		}
		if err := checkURL(c.RPCURL, true, production); err != nil {
			return nil, fmt.Errorf("%s: rpc_url %w", where, err)
		}
		if err := checkURL(c.PublicRPCURL, false, production); err != nil {
			return nil, fmt.Errorf("%s: public_rpc_url %w", where, err)
		}
		if err := checkURL(c.ExplorerURL, false, false); err != nil {
			return nil, fmt.Errorf("%s: explorer_url %w", where, err)
		}
		c.ExplorerURL = strings.TrimRight(c.ExplorerURL, "/")
		if c.NativeSymbol = strings.TrimSpace(c.NativeSymbol); c.NativeSymbol == "" {
			c.NativeSymbol = "ETH"
		}
		if c.NativeDecimals == nil {
			eighteen := 18
			c.NativeDecimals = &eighteen
		}
		if err := checkDecimals(*c.NativeDecimals); err != nil {
			return nil, fmt.Errorf("%s: native_decimals %w", where, err)
		}
		seen := map[Address]bool{}
		for j := range c.Tokens {
			t := &c.Tokens[j]
			a, err := ParseAddress(t.Address)
			if err != nil {
				return nil, fmt.Errorf("%s: tokens[%d].address: %w", where, j, err)
			}
			if seen[a] {
				return nil, fmt.Errorf("%s: token %s is listed twice", where, a.Hex())
			}
			seen[a] = true
			t.addr, t.Address = a, a.Hex()
			t.Symbol = strings.TrimSpace(t.Symbol)
			if n := len([]rune(t.Symbol)); n == 0 || n > 16 {
				return nil, fmt.Errorf("%s: token %s: symbol must be 1 to 16 characters", where, t.Address)
			}
			if t.Name = strings.TrimSpace(t.Name); t.Name == "" {
				t.Name = t.Symbol
			}
			if t.Decimals == nil {
				return nil, fmt.Errorf("%s: token %s: decimals is required (the token contract's decimals(), e.g. 6 for USDC)", where, t.Address)
			}
			if err := checkDecimals(*t.Decimals); err != nil {
				return nil, fmt.Errorf("%s: token %s: decimals %w", where, t.Address, err)
			}
		}
		r.byID[c.ID] = c
	}
	switch {
	case defaultID != 0:
		if _, ok := r.byID[defaultID]; !ok {
			return nil, fmt.Errorf("WALLETS_DEFAULT_CHAIN_ID %d is not in WALLETS_CHAINS", defaultID)
		}
		r.defaultID = defaultID
	case len(r.chains) > 0:
		r.defaultID = r.chains[0].ID
	}
	return r, nil
}

func checkDecimals(d int) error {
	if d < 0 || d > maxDecimals {
		return fmt.Errorf("must be a count of digits from 0 to %d (got %d; 6 means 1 token = 1000000 base units, so pass 6, not 1000000)", maxDecimals, d)
	}
	return nil
}

func checkURL(raw string, required, production bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			return errors.New("is required")
		}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("must be an http(s) URL")
	}
	if production && config.IsLocalURL(raw) {
		return errors.New("points at this machine; use a hosted endpoint in production")
	}
	return nil
}

// Chains returns the configured chains in config order.
func (r *Registry) Chains() []*Chain { return r.chains }

// Chain returns a configured chain.
func (r *Registry) Chain(id int64) (*Chain, bool) {
	c, ok := r.byID[id]
	return c, ok
}

// Token returns a configured token on a chain.
func (r *Registry) Token(chainID int64, a Address) (*Chain, *Token, bool) {
	c, ok := r.byID[chainID]
	if !ok {
		return nil, nil, false
	}
	for i := range c.Tokens {
		if c.Tokens[i].addr == a {
			return c, &c.Tokens[i], true
		}
	}
	return c, nil, false
}

// DefaultChainID is 0 when no chains are configured.
func (r *Registry) DefaultChainID() int64 { return r.defaultID }

// TxURL is the explorer link for a transaction, or nil.
func (r *Registry) TxURL(chainID int64, txHash string) *string {
	c, ok := r.byID[chainID]
	if !ok || c.ExplorerURL == "" {
		return nil
	}
	u := c.ExplorerURL + "/tx/" + txHash
	return &u
}

// ---- client view ------------------------------------------------------------

// ClientConfig is GET /v1/wallets/config. It never contains RPC secrets.
type ClientConfig struct {
	Chains         []PublicChain  `json:"chains"`
	DefaultChainID *int64         `json:"default_chain_id"`
	Embedded       EmbeddedConfig `json:"embedded"`
	MaxWallets     int            `json:"max_wallets"`
	// LinkURL is the web page where a wallet is linked (mobile opens it until
	// it links wallets itself): the SIWE origin + /settings/wallets.
	LinkURL string `json:"link_url"`
}

// EmbeddedConfig tells clients whether embedded (Privy) wallets work here.
// The Privy app id is public by design (the client SDK needs it).
type EmbeddedConfig struct {
	Enabled    bool    `json:"enabled"`
	Provider   *string `json:"provider"`
	PrivyAppID *string `json:"privy_app_id"`
}

type PublicChain struct {
	ID             int64         `json:"id"`
	Name           string        `json:"name"`
	PublicRPCURL   *string       `json:"public_rpc_url"`
	ExplorerURL    *string       `json:"explorer_url"`
	NativeSymbol   string        `json:"native_symbol"`
	NativeDecimals int           `json:"native_decimals"`
	Testnet        bool          `json:"testnet"`
	Tokens         []PublicToken `json:"tokens"`
}

type PublicToken struct {
	Address  string `json:"address"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals int    `json:"decimals"`
}

func (r *Registry) public() []PublicChain {
	out := make([]PublicChain, 0, len(r.chains))
	for _, c := range r.chains {
		pc := PublicChain{
			ID: c.ID, Name: c.Name, NativeSymbol: c.NativeSymbol, NativeDecimals: *c.NativeDecimals,
			Testnet: c.Testnet, PublicRPCURL: nonEmpty(c.PublicRPCURL), ExplorerURL: nonEmpty(c.ExplorerURL),
			Tokens: make([]PublicToken, 0, len(c.Tokens)),
		}
		for _, t := range c.Tokens {
			pc.Tokens = append(pc.Tokens, PublicToken{Address: t.Address, Symbol: t.Symbol, Name: t.Name, Decimals: *t.Decimals})
		}
		out = append(out, pc)
	}
	return out
}

func nonEmpty(s string) *string {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	return &s
}
