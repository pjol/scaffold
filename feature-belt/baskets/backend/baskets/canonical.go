package baskets

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// Canonical baskets. One basket unit holds Quantity units of each market:
//
//  1. merge the resolved lines by market symbol (quantities add up);
//  2. sort by symbol (byte order);
//  3. canonical string "basket-v1\n" + Σ symbol "\t" quantity "\n";
//     the hash is its lowercase hex SHA-256 (baskets.hash);
//  4. symbol = prefix + "-" + the first 6 Crockford base32 characters of the
//     hash bytes; 8, then 10, then 12 when a different basket (or another
//     market or feed) holds the shorter one.
//
// The same basket always maps to the same market, whoever creates it, and
// lists that differ only in order, spelling or line splits share one.

const canonicalPrefix = "basket-v1\n"

var crockford = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// SymbolLengths are the hash prefix lengths tried for a new symbol.
var SymbolLengths = []int{6, 8, 10, 12}

// Item is one canonical line.
type Item struct {
	Symbol   string
	Quantity int
}

// Canonical sorts items by symbol and returns the canonical string.
func Canonical(items []Item) string {
	ls := append([]Item(nil), items...)
	sort.Slice(ls, func(i, j int) bool { return ls[i].Symbol < ls[j].Symbol })
	var b strings.Builder
	b.WriteString(canonicalPrefix)
	for _, l := range ls {
		b.WriteString(l.Symbol)
		b.WriteByte('\t')
		b.WriteString(strconv.Itoa(l.Quantity))
		b.WriteByte('\n')
	}
	return b.String()
}

// Hash is the lowercase hex SHA-256 of the canonical string.
func Hash(items []Item) string {
	sum := sha256.Sum256([]byte(Canonical(items)))
	return hex.EncodeToString(sum[:])
}

// SymbolFor is prefix + "-" + the first n Crockford base32 characters of the hash bytes.
func SymbolFor(prefix, hash string, n int) string {
	raw, err := hex.DecodeString(hash)
	if err != nil {
		return ""
	}
	enc := crockford.EncodeToString(raw)
	return prefix + "-" + enc[:min(n, len(enc))]
}

// ---- the market spec (tick, sizes, leverage, open interest cap) ----

// Spec is the exchange market a new basket gets, from its value V (one
// basket unit, in the quote currency) and its largest line's share of V:
//
//	tick       the largest {1, 2, 5}×10^k ≤ V×10⁻⁴ (floor 0.001)
//	sz         2 size decimals below 1,000, else 3
//	leverage   BASKETS_MAX_LEVERAGE; about half (rounded up) when one line is over half of V
//	min order  BASKETS_MIN_ORDER
//	OI cap     100·V clamped to BASKETS_OI_CAP_MIN–MAX, 2 significant figures, halved when one line is over half of V
//
// A basket worth 312 gets tick 0.02, 2 size decimals and a 31,000 cap.
type Spec struct {
	TickSize    string `json:"tick_size"`
	SzDecimals  int    `json:"sz_decimals"`
	MaxLeverage int    `json:"max_leverage"`
	MinNotional string `json:"min_notional"`
	OICap       string `json:"oi_cap"`
}

var (
	tickFloor = decimal.RequireFromString("0.001")
	half      = decimal.RequireFromString("0.5")
)

// TickFor is the tick size for basket value v.
func TickFor(v decimal.Decimal) decimal.Decimal {
	limit := v.Shift(-4)
	if limit.LessThan(tickFloor) {
		return tickFloor
	}
	best := tickFloor
	for exp := int32(-3); exp <= 6; exp++ {
		for _, m := range []int64{1, 2, 5} {
			t := decimal.New(m, exp)
			if t.LessThanOrEqual(limit) && t.GreaterThan(best) {
				best = t
			}
		}
	}
	return best
}

// roundSig rounds a positive d to n significant figures.
func roundSig(d decimal.Decimal, n int32) decimal.Decimal {
	if !d.IsPositive() {
		return d
	}
	digits := int32(len(d.Truncate(0).String()))
	if d.LessThan(decimal.NewFromInt(1)) {
		digits = 0
	}
	return d.Round(n - digits)
}

// concentrated: one line holds more than half the value.
func concentrated(maxShare decimal.Decimal) bool { return maxShare.GreaterThan(half) }

// SpecFor is the spec of a basket worth v whose largest line is maxShare of it.
func (c settings) SpecFor(v, maxShare decimal.Decimal) Spec {
	sz := 3
	if v.LessThan(decimal.NewFromInt(1000)) {
		sz = 2
	}
	lev := c.maxLeverage
	oi := v.Mul(decimal.NewFromInt(100))
	if oi.LessThan(c.oiMin) {
		oi = c.oiMin
	}
	if oi.GreaterThan(c.oiMax) {
		oi = c.oiMax
	}
	oi = roundSig(oi, 2)
	if concentrated(maxShare) {
		lev = max(1, (lev+1)/2)
		oi = oi.Mul(half)
	}
	return Spec{TickSize: TickFor(v).String(), SzDecimals: sz, MaxLeverage: lev, MinNotional: c.minOrder.String(), OICap: oi.String()}
}
