package payments

import (
	"testing"
	"time"
)

func TestValidReturnPath(t *testing.T) {
	prefixes := []string{"/payments/", "/settings"}
	ok := map[string]string{
		"/payments/success":         "/payments/success",
		"/payments/cancel?plan=pro": "/payments/cancel?plan=pro",
		"/settings":                 "/settings",
		"/settings/billing":         "/settings/billing",
	}
	for in, want := range ok {
		got, err := validReturnPath("p", in, prefixes)
		if err != nil || got != want {
			t.Errorf("validReturnPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "payments/success", "//evil.example/payments/x", "https://evil.example/payments/x",
		"/settingsx", "/paymentsx", "/payments/../admin", "/payments/\\evil", "/payments/x#frag", "/admin",
	} {
		if got, err := validReturnPath("p", in, prefixes); err == nil {
			t.Errorf("validReturnPath(%q) = %q; want an error", in, got)
		}
	}
}

func TestWithParam(t *testing.T) {
	if got := withParam("/payments/success", "checkout_id", "a b"); got != "/payments/success?checkout_id=a+b" {
		t.Errorf("got %q", got)
	}
	if got := withParam("/payments/success?x=1", "k", "v"); got != "/payments/success?x=1&k=v" {
		t.Errorf("got %q", got)
	}
}

func TestFeeAmount(t *testing.T) {
	cases := []struct {
		amount int64
		bps    int
		want   int64
	}{
		{10000, 250, 250}, {999, 250, 24}, {0, 250, 0}, {5000, 0, 0}, {1, 10000, 1}, {-5, 100, 0},
	}
	for _, c := range cases {
		if got := feeAmount(c.amount, c.bps); got != c.want {
			t.Errorf("feeAmount(%d, %d) = %d; want %d", c.amount, c.bps, got, c.want)
		}
	}
}

func TestAccessUntil(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	end := now.Add(10 * 24 * time.Hour)
	grace := 24 * time.Hour
	cases := []struct {
		name string
		sub  Subscription
		want time.Time
	}{
		{"active renews with grace", Subscription{Status: SubActive, CurrentPeriodEnd: end}, end.Add(grace)},
		{"past due keeps access while retried", Subscription{Status: SubPastDue, CurrentPeriodEnd: end}, end.Add(grace)},
		{"cancel at period end lapses exactly", Subscription{Status: SubActive, CurrentPeriodEnd: end, CancelAtPeriodEnd: true}, end},
		{"scheduled cancel inside the period", Subscription{Status: SubActive, CurrentPeriodEnd: end, CancelAt: end.Add(-time.Hour)}, end.Add(-time.Hour)},
		{"scheduled cancel after the period renews", Subscription{Status: SubActive, CurrentPeriodEnd: end, CancelAt: end.Add(72 * time.Hour)}, end.Add(grace)},
		{"canceled ends at ended_at", Subscription{Status: SubCanceled, EndedAt: now.Add(-time.Hour)}, now.Add(-time.Hour)},
		{"unpaid ends now", Subscription{Status: SubUnpaid, CurrentPeriodEnd: end}, now},
		{"incomplete has no access", Subscription{Status: SubIncomplete, CurrentPeriodEnd: end}, now},
	}
	for _, c := range cases {
		if got := accessUntil(&c.sub, grace, now); !got.Equal(c.want) {
			t.Errorf("%s: accessUntil = %s; want %s", c.name, got, c.want)
		}
	}
}

func TestParseSeedPrices(t *testing.T) {
	got, err := parseSeedPrices(" pro_monthly=price_123:pro, lifetime=price_456 ,")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (seedEntry{"pro_monthly", "price_123", "pro"}) || got[1] != (seedEntry{"lifetime", "price_456", ""}) {
		t.Errorf("got %+v", got)
	}
	for _, bad := range []string{"pro", "Pro=price_1", "pro=", "pro=price_1:Bad-Key"} {
		if _, err := parseSeedPrices(bad); err == nil {
			t.Errorf("parseSeedPrices(%q): want an error", bad)
		}
	}
}

func TestOrderStatus(t *testing.T) {
	if orderStatus(1000, 0) != "paid" || orderStatus(1000, 400) != "partially_refunded" || orderStatus(1000, 1000) != "refunded" {
		t.Error("orderStatus thresholds wrong")
	}
}
