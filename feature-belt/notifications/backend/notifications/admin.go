package notifications

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

// Stats is GET /v1/admin/notifications/stats.
type Stats struct {
	PushMode         string         `json:"push_mode"` // expo | sink | log
	Notifications    int            `json:"notifications"`
	Notifications7d  int            `json:"notifications_7d"`
	Unread           int            `json:"unread"`
	Devices          map[string]int `json:"devices"` // by platform
	UsersWithDevices int            `json:"users_with_devices"`
	PendingReceipts  int            `json:"pending_receipts"`
	Kinds7d          []KindCount    `json:"kinds_7d"`
}

type KindCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

func (s *Service) adminStats(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	out := Stats{PushMode: s.sender.PushMode(), Devices: map[string]int{"ios": 0, "android": 0, "web": 0}, Kinds7d: []KindCount{}}
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE created_at > now() - interval '7 days'),
		       count(*) FILTER (WHERE read_at IS NULL)
		FROM notifications`).Scan(&out.Notifications, &out.Notifications7d, &out.Unread); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT platform, count(*) FROM notification_devices GROUP BY platform`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var platform string
		var n int
		if err := rows.Scan(&platform, &n); err != nil {
			rows.Close()
			return err
		}
		out.Devices[platform] = n
	}
	rows.Close()
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT (SELECT count(DISTINCT user_id) FROM notification_devices),
		       (SELECT count(*) FROM notification_push_tickets)`).Scan(&out.UsersWithDevices, &out.PendingReceipts); err != nil {
		return err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT kind, count(*) FROM notifications WHERE created_at > now() - interval '7 days'
		GROUP BY kind ORDER BY count(*) DESC, kind LIMIT 20`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var kc KindCount
		if err := rows.Scan(&kc.Kind, &kc.Count); err != nil {
			rows.Close()
			return err
		}
		out.Kinds7d = append(out.Kinds7d, kc)
	}
	rows.Close()
	httpx.OK(w, out)
	return nil
}

type sendTestReq struct {
	UserID uuid.UUID `json:"user_id"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
}

// TestResult is POST /v1/admin/notifications/test.
type TestResult struct {
	NotificationID uuid.UUID `json:"notification_id"`
	Devices        int       `json:"devices"`   // iOS/Android devices the push goes to
	PushMode       string    `json:"push_mode"` // expo | sink | log
}

// adminSendTest sends a notifications.test notification to a user: inbox,
// realtime and push to every device (preferences ignored, no email). Staff use
// it to check a user's push setup. Audited.
func (s *Service) adminSendTest(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	var req sendTestReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.UserID == uuid.Nil {
		return httpx.BadRequest("Choose a user.")
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		req.Title = "Test notification"
	}
	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" {
		req.Body = "Sent from the admin console."
	}
	if len([]rune(req.Title)) > maxTitleRunes || len([]rune(req.Body)) > 500 {
		return httpx.BadRequest("Keep the title under 200 characters and the body under 500.")
	}
	var exists bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND status <> 'deleted')`,
		req.UserID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return httpx.NotFound("No user with that id.")
	}
	if err := audit.Log(ctx, s.d.Pool, staff.UserID, "notifications.test", "user", req.UserID.String(),
		map[string]string{"title": req.Title}); err != nil {
		return err
	}
	res, err := s.sender.deliver(context.WithoutCancel(ctx), notify.Notification{
		UserID: req.UserID, Kind: kindTest, Title: req.Title, Body: req.Body,
	}, deliverOpts{forcePush: true, noEmail: true})
	if errors.Is(err, errRecipientGone) {
		return httpx.NotFound("No user with that id.")
	}
	if err != nil {
		return err
	}
	httpx.OK(w, TestResult{NotificationID: res.Item.ID, Devices: res.Devices, PushMode: s.sender.PushMode()})
	return nil
}
