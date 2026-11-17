package calls

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// actor is someone acting inside a meeting's call: a person who may join
// it (user routes, /calls/meetings/{id}/…) or a guest a host admitted
// (public routes, /calls/public/lobby/{id}/… with {key} in the body). The
// in-call features (polls, questions, transcript lines, saved chat,
// breakout rooms) take an actor so both doors run the same code.
type actor struct {
	m        *meetingRow
	identity string
	userID   *uuid.UUID
	name     string
	host     bool // host or co-host
	role     string
	avatar   *string
}

type actFn func(w http.ResponseWriter, r *http.Request, a *actor) error

// userAct runs fn for a signed-in person who may join meeting {id}.
func (s *Service) userAct(fn actFn) http.HandlerFunc {
	return httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		me := auth.MustFromContext(ctx)
		m, err := s.meetingParam(r)
		if err != nil {
			return err
		}
		a, err := s.accessFor(ctx, s.d.Pool, m, me.UserID)
		if err != nil {
			return err
		}
		if !a.canJoin() {
			if a.Mode == ModeKnock {
				return joinRefusal(a)
			}
			return errMeetingNotFound
		}
		var name string
		var avatar *string
		if err := s.d.Pool.QueryRow(ctx, `SELECT display_name, avatar_url FROM users WHERE id = $1`, me.UserID).Scan(&name, &avatar); err != nil {
			return err
		}
		uid := me.UserID
		return fn(w, r, &actor{m: m, identity: userIdentity(uid), userID: &uid, name: name, host: a.isHost(), role: a.Role, avatar: avatar})
	})
}

// guestAct runs fn for an admitted guest: lobby entry {id}, its key in the
// body's "key" (removed before fn decodes the rest).
func (s *Service) guestAct(fn actFn) http.HandlerFunc {
	return httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			return errLobbyNotFound
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			return httpx.BadRequest("Couldn't read the body.")
		}
		fields := map[string]json.RawMessage{}
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &fields); err != nil {
				return httpx.BadRequest("Invalid JSON: " + err.Error())
			}
		}
		var key string
		_ = json.Unmarshal(fields["key"], &key)
		if !strings.HasPrefix(key, "cgk_") {
			return errLobbyNotFound
		}
		delete(fields, "key")
		rest, _ := json.Marshal(fields)
		r.Body = io.NopCloser(bytes.NewReader(rest))
		list, err := loadLobby(ctx, s.d.Pool, `WHERE l.id = $1 AND `+guestOwner, id, hashKey(key))
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return errLobbyNotFound
		}
		m, err := s.admittedGuest(ctx, list[0])
		if err != nil {
			return err
		}
		return fn(w, r, &actor{m: m, identity: guestIdentity(id), name: list[0].Name, role: RoleGuest})
	})
}

// requireLive refuses actions that need the call to be running.
func (a *actor) requireLive() error {
	if a.m.LiveSession == nil {
		return errNotLive
	}
	return nil
}

func (a *actor) requireHost() error {
	if !a.host {
		return errNotHost
	}
	return nil
}
