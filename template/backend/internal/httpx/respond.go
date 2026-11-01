// Package httpx holds HTTP plumbing shared by every domain package: JSON
// responses, the error envelope, request decoding, pagination, and middleware.
package httpx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Error is a typed API error. Handlers return it (or wrap it) and Fail renders
// it as {"error":{"code","message"}} with Status.
type Error struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func NewError(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

// Common errors. Prefer these (or the constructors below) over ad-hoc codes.
var (
	ErrUnauthenticated = NewError(http.StatusUnauthorized, "unauthenticated", "Please sign in.")
	ErrForbidden       = NewError(http.StatusForbidden, "forbidden", "You don't have access to that.")
	ErrNotFound        = NewError(http.StatusNotFound, "not_found", "Not found.")
	ErrSuspended       = NewError(http.StatusForbidden, "account_suspended", "Your account is suspended. Contact support.")
	ErrRateLimited     = NewError(http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again shortly.")
)

func BadRequest(msg string) *Error { return NewError(http.StatusBadRequest, "invalid_request", msg) }
func Conflict(msg string) *Error   { return NewError(http.StatusConflict, "conflict", msg) }
func NotFound(msg string) *Error   { return NewError(http.StatusNotFound, "not_found", msg) }
func Forbidden(msg string) *Error  { return NewError(http.StatusForbidden, "forbidden", msg) }

// JSON writes v with status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// OK writes 200 with v.
func OK(w http.ResponseWriter, v any) { JSON(w, http.StatusOK, v) }

// NoContent writes 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// Fail renders err. *Error values are rendered as-is; anything else is logged
// with the request context and rendered as a generic 500.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	// The client went away (navigation, timeout on their side): nobody reads the
	// response, so this is not a server failure. 499 is nginx's "client closed".
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		slog.DebugContext(r.Context(), "request canceled by client", "method", r.Method, "path", r.URL.Path)
		w.WriteHeader(499)
		return
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		if apiErr.Status >= 500 {
			slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		}
		JSON(w, apiErr.Status, map[string]any{"error": apiErr})
		return
	}
	slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path,
		"request_id", RequestIDFrom(r.Context()), "err", fmt.Sprintf("%+v", err))
	JSON(w, http.StatusInternalServerError, map[string]any{
		"error": &Error{Code: "internal", Message: "Something went wrong. Please try again."},
	})
}

// Handler adapts a func returning error into an http.HandlerFunc, so handlers
// can `return httpx.BadRequest("...")` instead of writing responses by hand.
func Handler(fn func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			Fail(w, r, err)
		}
	}
}

// Decode parses a JSON body (max 1 MiB) into dst, rejecting unknown fields.
// An empty body is treated as {} — clients send no body for pure actions and
// {} when every field is optional — so handlers must validate required fields.
func Decode(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return BadRequest("Invalid JSON: " + err.Error())
	}
	return nil
}

// URLParamUUID parses a chi URL param as a UUID.
func URLParamUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, BadRequest("Invalid " + name + ".")
	}
	return id, nil
}

// QueryFloat parses an optional float query param.
func QueryFloat(r *http.Request, name string) (float64, bool, error) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return 0, false, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false, BadRequest("Invalid " + name + ".")
	}
	return f, true, nil
}

// Page is the list envelope: {"items":[...],"next_cursor":...}.
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// PageParams are parsed ?limit=&cursor= values.
type PageParams struct {
	Limit  int
	Cursor string // opaque; decode with DecodeCursor
}

// ParsePage reads limit (default 20, max 100) and cursor.
func ParsePage(r *http.Request) PageParams {
	p := PageParams{Limit: 20, Cursor: r.URL.Query().Get("cursor")}
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		p.Limit = min(v, 100)
	}
	return p
}

// EncodeCursor / DecodeCursor wrap an opaque keyset cursor (any JSON value,
// typically {"t": created_at, "id": uuid}).
func EncodeCursor(v any) *string {
	b, _ := json.Marshal(v)
	s := base64.RawURLEncoding.EncodeToString(b)
	return &s
}

func DecodeCursor(s string, dst any) error {
	if s == "" {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return BadRequest("Invalid cursor.")
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return BadRequest("Invalid cursor.")
	}
	return nil
}

// NewPage builds a Page from rows fetched with limit+1 (the extra row signals
// another page). cursorOf builds the cursor from the last returned item.
func NewPage[T any](rows []T, limit int, cursorOf func(T) any) Page[T] {
	if rows == nil {
		rows = []T{}
	}
	if len(rows) > limit {
		rows = rows[:limit]
		return Page[T]{Items: rows, NextCursor: EncodeCursor(cursorOf(rows[len(rows)-1]))}
	}
	return Page[T]{Items: rows}
}
