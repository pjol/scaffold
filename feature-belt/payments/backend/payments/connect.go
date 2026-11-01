package payments

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Connect (PAYMENTS_CONNECT_ENABLED=true): a user who sells gets an Express
// connected account; prices with a destination (or stitched checkouts with
// DestinationUserID) send the money there minus PAYMENTS_PLATFORM_FEE_BPS.

// ConnectedAccount is a payout account as its owner sees it.
type ConnectedAccount struct {
	ChargesEnabled   bool      `json:"charges_enabled"`
	PayoutsEnabled   bool      `json:"payouts_enabled"`
	DetailsSubmitted bool      `json:"details_submitted"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (s *Service) loadAccount(ctx context.Context, userID uuid.UUID) (string, *ConnectedAccount, error) {
	var id string
	var a ConnectedAccount
	err := s.d.Pool.QueryRow(ctx, `SELECT provider_account_id, charges_enabled, payouts_enabled, details_submitted,
		created_at, updated_at FROM payment_connected_accounts WHERE user_id = $1 AND provider = $2`,
		userID, s.gw.Name()).Scan(&id, &a.ChargesEnabled, &a.PayoutsEnabled, &a.DetailsSubmitted, &a.CreatedAt, &a.UpdatedAt)
	if db.IsNoRows(err) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	return id, &a, nil
}

// GET /v1/payments/connect/account → {"account": …|null}. An account still
// in onboarding is re-read from the provider, so the page is current even
// if a webhook was missed.
func (s *Service) getConnectedAccount(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	id, acct, err := s.loadAccount(r.Context(), caller.UserID)
	if err != nil {
		return err
	}
	if acct != nil && !(acct.ChargesEnabled && acct.PayoutsEnabled) {
		fresh, err := s.gw.GetAccount(r.Context(), id)
		if err != nil {
			return providerFailure(r.Context(), "get_account", err)
		}
		if _, err := s.d.Pool.Exec(r.Context(), `UPDATE payment_connected_accounts SET charges_enabled = $2,
			payouts_enabled = $3, details_submitted = $4, updated_at = now() WHERE user_id = $1 AND provider = $5`,
			caller.UserID, fresh.ChargesEnabled, fresh.PayoutsEnabled, fresh.DetailsSubmitted, s.gw.Name()); err != nil {
			return err
		}
		if _, acct, err = s.loadAccount(r.Context(), caller.UserID); err != nil {
			return err
		}
	}
	httpx.OK(w, map[string]any{"account": acct})
	return nil
}

type onboardingReq struct {
	ReturnPath string `json:"return_path"`
}

// POST /v1/payments/connect/onboarding → {"url"}: creates the account on
// first use and returns a hosted onboarding link. The provider sends the
// user back to return_path (default /payments/payouts); an expired link
// lands on the same path with ?onboarding=refresh to request a new one.
func (s *Service) connectOnboarding(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	var req onboardingReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.ReturnPath == "" {
		req.ReturnPath = "/payments/payouts"
	}
	path, err := validReturnPath("return_path", req.ReturnPath, s.cfg.returnPaths)
	if err != nil {
		return err
	}
	returnURL, err := s.webURL(withParam(path, "onboarding", "return"))
	if err != nil {
		return err
	}
	refreshURL, err := s.webURL(withParam(path, "onboarding", "refresh"))
	if err != nil {
		return err
	}
	acctID, _, err := s.loadAccount(r.Context(), caller.UserID)
	if err != nil {
		return err
	}
	if acctID == "" {
		a, err := s.gw.CreateAccount(r.Context(), AccountInput{UserID: caller.UserID.String(), Email: caller.Email},
			"account:"+caller.UserID.String())
		if err != nil {
			return providerFailure(r.Context(), "create_account", err)
		}
		if err := s.d.Pool.QueryRow(r.Context(), `
			WITH ins AS (
				INSERT INTO payment_connected_accounts (user_id, provider, provider_account_id, charges_enabled,
				  payouts_enabled, details_submitted)
				VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (user_id, provider) DO NOTHING
				RETURNING provider_account_id)
			SELECT provider_account_id FROM ins
			UNION ALL
			SELECT provider_account_id FROM payment_connected_accounts WHERE user_id = $1 AND provider = $2
			LIMIT 1`, caller.UserID, s.gw.Name(), a.ID, a.ChargesEnabled, a.PayoutsEnabled, a.DetailsSubmitted).
			Scan(&acctID); err != nil {
			return err
		}
	}
	link, err := s.gw.OnboardingLink(r.Context(), acctID, refreshURL, returnURL)
	if err != nil {
		return providerFailure(r.Context(), "onboarding_link", err)
	}
	httpx.OK(w, map[string]string{"url": link})
	return nil
}
