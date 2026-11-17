package crm

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Line-item arithmetic, all in integer minor units. Each line rounds half
// away from zero once per step (subtotal, discount, tax), and totals are sums
// of rounded lines, so a quote's total always equals the sum of what its
// lines show.

const (
	maxMoney    = int64(1e15) // a line or total above this is refused
	maxQuantity = int64(1e9)  // thousandths: 1,000,000 units
)

// billingMonths is the length of a recurring billing period.
var billingMonths = map[string]int{"monthly": 1, "quarterly": 3, "semiannual": 6, "annual": 12}

// lineInput is what a line's totals depend on.
type lineInput struct {
	UnitPrice      int64 // minor units
	QuantityMilli  int64 // thousandths
	DiscountBP     int   // percent discount in basis points (0–10000)
	DiscountAmount int64 // fixed discount on the line, minor units (or DiscountBP, not both)
	TaxBP          int   // basis points on the discounted amount
	Billing        string
	BillingPeriod  string
	TermPeriods    int // recurring: how many billing periods the contract runs
}

// LineTotals is one line's money: subtotal = unit price × quantity, then the
// discount, the tax on what's left, the total for one billing period, and the
// contract value (total × term for recurring lines).
type LineTotals struct {
	Subtotal      int64 `json:"subtotal"`
	Discount      int64 `json:"discount"`
	Tax           int64 `json:"tax"`
	Total         int64 `json:"total"`
	ContractValue int64 `json:"contract_value"`
}

// divRound returns n/d rounded half away from zero (d > 0).
func divRound(n, d *big.Int) *big.Int {
	q, r := new(big.Int).QuoRem(n, d, new(big.Int))
	twice := new(big.Int).Mul(new(big.Int).Abs(r), big.NewInt(2))
	if twice.Cmp(d) >= 0 {
		if n.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}

// mulDiv is round(a × b / d) without overflow; ok is false above maxMoney.
func mulDiv(a, b, d int64) (int64, bool) {
	n := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	q := divRound(n, big.NewInt(d))
	if !q.IsInt64() || q.Int64() > maxMoney || q.Int64() < -maxMoney {
		return 0, false
	}
	return q.Int64(), true
}

// totals computes a line's money; ok is false when an amount is too large.
func (l lineInput) totals() (LineTotals, bool) {
	var t LineTotals
	sub, ok := mulDiv(l.UnitPrice, l.QuantityMilli, 1000)
	if !ok {
		return t, false
	}
	t.Subtotal = sub
	switch {
	case l.DiscountBP > 0:
		t.Discount, _ = mulDiv(sub, int64(l.DiscountBP), 10000)
	case l.DiscountAmount > 0:
		t.Discount = min(l.DiscountAmount, sub)
	}
	net := sub - t.Discount
	t.Tax, _ = mulDiv(net, int64(l.TaxBP), 10000)
	t.Total = net + t.Tax
	term := int64(1)
	if l.Billing == "recurring" && l.TermPeriods > 0 {
		term = int64(l.TermPeriods)
	}
	if t.Total > maxMoney/term {
		return t, false
	}
	t.ContractValue = t.Total * term
	return t, true
}

// RecurringTotal is the per-period total of the recurring lines billed on
// one schedule.
type RecurringTotal struct {
	BillingPeriod string `json:"billing_period"`
	Total         int64  `json:"total"`
}

// Totals sums lines: one-time lines plus one billing period of each
// recurring line make up subtotal…total; contract value counts each
// recurring line for its whole term (what a deal's amount follows).
type Totals struct {
	Subtotal      int64            `json:"subtotal"`
	Discount      int64            `json:"discount"`
	Tax           int64            `json:"tax"`
	Total         int64            `json:"total"`
	OneTime       int64            `json:"one_time"`
	Recurring     []RecurringTotal `json:"recurring"`
	ContractValue int64            `json:"contract_value"`
}

// sumLines adds up lines; ok is false when a line or the sum is too large.
func sumLines(lines []lineInput) (Totals, []LineTotals, bool) {
	t := Totals{Recurring: []RecurringTotal{}}
	per := map[string]int64{}
	out := make([]LineTotals, len(lines))
	for i, l := range lines {
		lt, ok := l.totals()
		if !ok {
			return t, out, false
		}
		out[i] = lt
		t.Subtotal += lt.Subtotal
		t.Discount += lt.Discount
		t.Tax += lt.Tax
		t.Total += lt.Total
		t.ContractValue += lt.ContractValue
		if l.Billing == "recurring" {
			per[l.BillingPeriod] += lt.Total
		} else {
			t.OneTime += lt.Total
		}
		if t.Subtotal > maxMoney || t.ContractValue > maxMoney {
			return t, out, false
		}
	}
	for _, p := range []string{"monthly", "quarterly", "semiannual", "annual"} {
		if v, ok := per[p]; ok {
			t.Recurring = append(t.Recurring, RecurringTotal{BillingPeriod: p, Total: v})
		}
	}
	return t, out, true
}

// parseQuantity reads a quantity with up to three decimals ("1.5", 2) into
// thousandths.
func parseQuantity(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		return 0, false
	}
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 3 || whole == "" && frac == "" {
		return 0, false
	}
	for len(frac) < 3 {
		frac += "0"
	}
	w, err := strconv.ParseInt(whole+"", 10, 64)
	if whole == "" {
		w, err = 0, nil
	}
	if err != nil || w > maxQuantity/1000 {
		return 0, false
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, false
	}
	q := w*1000 + f
	return q, q > 0 && q <= maxQuantity
}

// quantityValue is thousandths as a JSON number (1500 → 1.5).
func quantityValue(milli int64) float64 {
	return math.Round(float64(milli)) / 1000
}

// formatQuantity is thousandths as text people read (1500 → "1.5").
func formatQuantity(milli int64) string {
	s := strconv.FormatInt(milli/1000, 10)
	if f := milli % 1000; f != 0 {
		s += "." + strings.TrimRight(strconv.FormatInt(1000+f, 10)[1:], "0")
	}
	return s
}

// formatPercent is basis points as text (825 → "8.25 %").
func formatPercent(bp int) string {
	s := strconv.Itoa(bp / 100)
	if f := bp % 100; f != 0 {
		s += "." + strings.TrimRight(strconv.Itoa(100+f)[1:], "0")
	}
	return s + " %"
}
