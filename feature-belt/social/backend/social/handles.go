package social

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Handle rules: 3–20 characters, lowercase letters, digits and '_', starting
// with a letter. Input is lowercased first, so "@Sam_Lee" asks for sam_lee.
const (
	HandleMin = 3
	HandleMax = 20
)

// reservedHandles can't be taken by anyone: routes, roles and words that
// would mislead. SOCIAL_RESERVED_HANDLES adds more.
var reservedHandles = []string{
	"about", "account", "admin", "administrator", "api", "app", "auth", "billing", "blog", "contact", "dashboard",
	"delete", "edit", "everyone", "explore", "feed", "followers", "following", "help", "home", "info", "legal",
	"login", "logout", "me", "mod", "moderator", "new", "news", "notifications", "null", "official", "people",
	"privacy", "profile", "register", "root", "search", "security", "settings", "signin", "signout", "signup",
	"staff", "support", "system", "team", "terms", "undefined", "user", "users", "www", "you", "scaffoldapp",
}

// protectedNames can't be imitated: a handle resembling one of them (after
// folding case, accents and letter swaps like 0→o) is refused, except for
// staff. The app's own name is one of them.
var protectedNames = []string{"admin", "administrator", "moderator", "official", "staff", "support", "security", "system", "scaffoldapp"}

// Handle problems, as API errors.
var (
	errHandleInvalid = httpx.NewError(http.StatusUnprocessableEntity, "handle_invalid",
		fmt.Sprintf("Handles are %d–%d letters, numbers or underscores, starting with a letter.", HandleMin, HandleMax))
	errHandleReserved = httpx.NewError(http.StatusUnprocessableEntity, "handle_reserved", "That handle is reserved. Try another.")
	errHandleTaken    = httpx.NewError(http.StatusConflict, "handle_taken", "That handle is taken. Try another.")
)

// NormalizeHandle trims, drops a leading "@" and lowercases.
func NormalizeHandle(h string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(h), "@"))
}

// ValidHandle reports whether h (already normalised) has the handle shape.
func ValidHandle(h string) bool {
	if len(h) < HandleMin || len(h) > HandleMax || h[0] < 'a' || h[0] > 'z' {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// checkHandle returns the reason h can't be chosen by this person, or nil.
// extra are the SOCIAL_RESERVED_HANDLES; staff skip the lookalike check.
func checkHandle(h string, extra []string, staff bool) *httpx.Error {
	if !ValidHandle(h) {
		return errHandleInvalid
	}
	for _, r := range reservedHandles {
		if h == r {
			return errHandleReserved
		}
	}
	for _, r := range extra {
		if h == NormalizeHandle(r) {
			return errHandleReserved
		}
	}
	if !staff && resemblesProtected(h, protectedNames) {
		return errHandleReserved
	}
	return nil
}

var leet = strings.NewReplacer("0", "o", "1", "i", "3", "e", "4", "a", "5", "s", "7", "t", "@", "a", "$", "s", "!", "i", "|", "l")

// foldName folds case, accents and letter swaps so lookalikes compare equal
// ("Supp0rt" → "support"), keeping only a–z and 0–9.
func foldName(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	if folded, _, err := transform.String(t, s); err == nil {
		s = folded
	}
	s = leet.Replace(strings.ToLower(s))
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// resemblesProtected reports whether name, or any '_'-separated word of it,
// folds to a protected name or near it: exact for names of up to 5 letters
// ("adm1n", "ST4FF"), one edit for 6–8 ("suport"), two from 9 ("moderatr").
// Short names stay exact so ordinary words ("stuff", "admit") pass.
func resemblesProtected(name string, protected []string) bool {
	cands := []string{foldName(name)}
	for _, w := range strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' || r == '.' || unicode.IsSpace(r) }) {
		if f := foldName(w); f != "" && f != cands[0] {
			cands = append(cands, f)
		}
	}
	for _, p := range protected {
		pn := foldName(p)
		if pn == "" {
			continue
		}
		maxd := 2
		switch {
		case len(pn) <= 5:
			maxd = 0
		case len(pn) <= 8:
			maxd = 1
		}
		for _, c := range cands {
			if c != "" && levenshtein(c, pn) <= maxd {
				return true
			}
		}
	}
	return false
}

// handleBase turns a display name or email into a handle stem: folded to
// a–z, 0–9 and single underscores, starting with a letter, at most 14
// characters (room for a numeric suffix). "" when nothing usable is left.
func handleBase(name string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	if folded, _, err := transform.String(t, name); err == nil {
		name = folded
	}
	var b strings.Builder
	sep := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if sep && b.Len() > 0 {
				b.WriteByte('_')
			}
			sep = false
			if b.Len() == 0 && r >= '0' && r <= '9' {
				continue // must start with a letter
			}
			b.WriteRune(r)
		default:
			sep = true
		}
		if b.Len() >= 14 {
			break
		}
	}
	out := strings.Trim(b.String(), "_")
	if len(out) > 14 {
		out = strings.TrimRight(out[:14], "_")
	}
	if len(out) < HandleMin {
		return ""
	}
	return out
}

// handleCandidates are the handles tried, in order, for a new profile.
func handleCandidates(displayName, email string, id uuid.UUID) []string {
	local, _, _ := strings.Cut(email, "@")
	first, _, _ := strings.Cut(strings.TrimSpace(displayName), " ")
	// The whole name, else its first word ("Avery Admin" → avery), else the
	// email's local part, else "user".
	base := "user"
	for _, b := range []string{handleBase(displayName), handleBase(first), handleBase(local)} {
		if b != "" && checkHandle(b, nil, false) == nil {
			base = b
			break
		}
	}
	out := []string{}
	if checkHandle(base, nil, false) == nil {
		out = append(out, base)
	}
	for i := 0; i < 3; i++ {
		out = append(out, fmt.Sprintf("%s%d", base, 10+randN(90)))
	}
	for i := 0; i < 4; i++ {
		out = append(out, fmt.Sprintf("%s%d", base, 1000+randN(9000)))
	}
	hex := strings.ReplaceAll(id.String(), "-", "")
	out = append(out, "user_"+hex[:12])
	return out
}

func randN(n int) int {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int(binary.BigEndian.Uint64(b[:]) % uint64(n))
}

// ensureProfiles gives every listed account that has none a profile with a
// generated handle. Idempotent and safe inside a transaction (conflicts are
// skipped, never raised).
func ensureProfiles(ctx context.Context, q db.Querier, ids []uuid.UUID) error {
	rows, err := q.Query(ctx, `
		SELECT u.id, u.display_name, u.email FROM users u
		WHERE u.id = ANY($1) AND u.status <> 'deleted'
		  AND NOT EXISTS (SELECT 1 FROM social_profiles p WHERE p.user_id = u.id)`, ids)
	if err != nil {
		return err
	}
	type missing struct {
		id          uuid.UUID
		name, email string
	}
	var todo []missing
	for rows.Next() {
		var m missing
		if err := rows.Scan(&m.id, &m.name, &m.email); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range todo {
		if err := createProfile(ctx, q, m.id, m.name, m.email); err != nil {
			return err
		}
	}
	return nil
}

func createProfile(ctx context.Context, q db.Querier, id uuid.UUID, name, email string) error {
	for _, h := range handleCandidates(name, email, id) {
		tag, err := q.Exec(ctx, `
			INSERT INTO social_profiles (user_id, handle)
			SELECT $1, $2 WHERE NOT EXISTS (SELECT 1 FROM social_handle_history h WHERE h.handle = $2 AND h.expires_at > now())
			ON CONFLICT DO NOTHING`, id, h)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			return nil
		}
		var exists bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM social_profiles WHERE user_id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil // made by a concurrent request
		}
	}
	return fmt.Errorf("social: no free handle for %s", id)
}

// resolveHandle finds who holds handle now, or held it within the redirect
// window. current is the holder's current handle ("me" resolves nothing;
// callers map it to the caller first).
func resolveHandle(ctx context.Context, q db.Querier, handle string) (id uuid.UUID, current string, err error) {
	h := NormalizeHandle(handle)
	if !ValidHandle(h) {
		return uuid.Nil, "", ErrNotFound
	}
	err = q.QueryRow(ctx, `SELECT user_id, handle FROM social_profiles WHERE handle = $1`, h).Scan(&id, &current)
	if err == nil {
		return id, current, nil
	}
	if !isNoRows(err) {
		return uuid.Nil, "", err
	}
	err = q.QueryRow(ctx, `
		SELECT p.user_id, p.handle FROM social_handle_history h JOIN social_profiles p ON p.user_id = h.user_id
		WHERE h.handle = $1 AND h.expires_at > now()`, h).Scan(&id, &current)
	if isNoRows(err) {
		return uuid.Nil, "", ErrNotFound
	}
	return id, current, err
}

// handleState is whether h can be claimed by me right now.
func handleFree(ctx context.Context, q db.Querier, me uuid.UUID, h string) (bool, error) {
	var free bool
	err := q.QueryRow(ctx, `
		SELECT NOT EXISTS (SELECT 1 FROM social_profiles WHERE handle = $2 AND user_id <> $1)
		   AND NOT EXISTS (SELECT 1 FROM social_handle_history WHERE handle = $2 AND expires_at > now()
		                   AND user_id IS DISTINCT FROM $1)`, me, h).Scan(&free)
	return free, err
}

// changeHandle moves me to handle h in tx: the cooldown applies, the old
// handle redirects (and stays reserved for me) for redirect, and taking back
// one of my own old handles is allowed.
func changeHandle(ctx context.Context, q db.Querier, me uuid.UUID, h string, cooldown, redirect time.Duration) (bool, error) {
	var cur string
	var changed *time.Time
	if err := q.QueryRow(ctx, `SELECT handle, handle_changed_at FROM social_profiles WHERE user_id = $1 FOR UPDATE`, me).Scan(&cur, &changed); err != nil {
		return false, err
	}
	if cur == h {
		return false, nil
	}
	if changed != nil && cooldown > 0 {
		if next := changed.Add(cooldown); time.Now().Before(next) {
			e := httpx.NewError(http.StatusTooManyRequests, "handle_cooldown",
				"You changed your handle recently. You can change it again on "+next.UTC().Format("Jan 2, 2006")+".")
			e.Details = map[string]any{"available_at": next.UTC()}
			return false, e
		}
	}
	free, err := handleFree(ctx, q, me, h)
	if err != nil {
		return false, err
	}
	if !free {
		return false, errHandleTaken
	}
	if _, err := q.Exec(ctx, `DELETE FROM social_handle_history WHERE handle = $1`, h); err != nil {
		return false, err
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO social_handle_history (handle, user_id, expires_at) VALUES ($1, $2, now() + $3::interval)
		ON CONFLICT (handle) DO UPDATE SET user_id = EXCLUDED.user_id, created_at = now(), expires_at = EXCLUDED.expires_at`,
		cur, me, interval(redirect)); err != nil {
		return false, err
	}
	_, err = q.Exec(ctx, `UPDATE social_profiles SET handle = $2, handle_changed_at = now(), updated_at = now() WHERE user_id = $1`, me, h)
	if db.IsUniqueViolation(err, "social_profiles_handle_key") {
		return false, errHandleTaken
	}
	return err == nil, err
}

// interval renders d for a Postgres interval parameter.
func interval(d time.Duration) string { return fmt.Sprintf("%d seconds", int64(d/time.Second)) }
