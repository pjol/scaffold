package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	clerkuser "github.com/clerk/clerk-sdk-go/v2/user"
)

// Clerk is the default Provider. Session tokens are verified locally against
// Clerk's JWKS (cached per key id); the Clerk API is called only to provision
// a first-time user and to delete one.
type Clerk struct {
	parties map[string]struct{} // allowed azp origins; empty = any

	mu   sync.Mutex
	keys map[string]cachedKey
}

type cachedKey struct {
	jwk     *clerk.JSONWebKey
	fetched time.Time
}

const jwkTTL = time.Hour

// NewClerk configures the Clerk SDK with secretKey. authorizedParties are our
// web origins: a token minted for another site on the same Clerk instance is
// refused. Tokens without azp (native apps) pass.
func NewClerk(secretKey string, authorizedParties ...string) *Clerk {
	clerk.SetKey(secretKey)
	c := &Clerk{parties: map[string]struct{}{}, keys: map[string]cachedKey{}}
	for _, o := range authorizedParties {
		if n := NormalizeOrigin(o); n != "" {
			c.parties[n] = struct{}{}
		}
	}
	return c
}

func (c *Clerk) Subject(ctx context.Context, _ *http.Request, token string) (string, error) {
	unverified, err := jwt.Decode(ctx, &jwt.DecodeParams{Token: token})
	if err != nil {
		return "", err
	}
	jwk, err := c.key(ctx, unverified.KeyID)
	if err != nil {
		return "", err
	}
	claims, err := jwt.Verify(ctx, &jwt.VerifyParams{
		Token:                  token,
		JWK:                    jwk,
		Leeway:                 5 * time.Second,
		AuthorizedPartyHandler: c.authorizedParty,
	})
	if err != nil {
		return "", err
	}
	return claims.Subject, nil
}

func (c *Clerk) key(ctx context.Context, kid string) (*clerk.JSONWebKey, error) {
	if kid == "" {
		return nil, errors.New("token has no key id")
	}
	c.mu.Lock()
	k, ok := c.keys[kid]
	c.mu.Unlock()
	if ok && time.Since(k.fetched) < jwkTTL {
		return k.jwk, nil
	}
	jwk, err := jwt.GetJSONWebKey(ctx, &jwt.GetJSONWebKeyParams{KeyID: kid})
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.keys[kid] = cachedKey{jwk: jwk, fetched: time.Now()}
	c.mu.Unlock()
	return jwk, nil
}

func (c *Clerk) authorizedParty(azp string) bool {
	if azp == "" || len(c.parties) == 0 {
		return true
	}
	if _, wild := c.parties["*"]; wild {
		return true
	}
	_, ok := c.parties[NormalizeOrigin(azp)]
	return ok
}

func (c *Clerk) Profile(ctx context.Context, subject string) (Profile, error) {
	u, err := clerkuser.Get(ctx, subject)
	if err != nil {
		return Profile{}, err
	}
	p := Profile{Email: verifiedEmail(u)}
	var parts []string
	if u.FirstName != nil && *u.FirstName != "" {
		parts = append(parts, *u.FirstName)
	}
	if u.LastName != nil && *u.LastName != "" {
		parts = append(parts, *u.LastName)
	}
	p.DisplayName = strings.Join(parts, " ")
	if p.DisplayName == "" && u.Username != nil {
		p.DisplayName = *u.Username
	}
	if u.ImageURL != nil {
		p.AvatarURL = *u.ImageURL
	}
	return p, nil
}

func (c *Clerk) DeleteUser(ctx context.Context, subject string) error {
	_, err := clerkuser.Delete(ctx, subject)
	return err
}

// verifiedEmail is the primary email if verified, else the first verified
// one, else "".
func verifiedEmail(u *clerk.User) string {
	verified := func(e *clerk.EmailAddress) bool {
		return e != nil && e.EmailAddress != "" && e.Verification != nil && e.Verification.Status == "verified"
	}
	if u.PrimaryEmailAddressID != nil {
		for _, e := range u.EmailAddresses {
			if e != nil && e.ID == *u.PrimaryEmailAddressID && verified(e) {
				return strings.ToLower(e.EmailAddress)
			}
		}
	}
	for _, e := range u.EmailAddresses {
		if verified(e) {
			return strings.ToLower(e.EmailAddress)
		}
	}
	return ""
}
