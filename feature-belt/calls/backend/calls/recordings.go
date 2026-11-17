package calls

import (
	"context"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/storage"
)

// Recordings: a host starts and stops an SFU recording (LiveKit Egress,
// room composite, one MP4 written straight to the app's S3 bucket); the
// room's metadata tells everyone in it, the pre-join screens say so, and
// egress_* webhooks finish the row. Who may open a recording is the
// meeting's records rule (recordsVisibleSQL), the same for transcripts and
// saved chat.

// KindNotifyRecording tells whoever started a recording that it is ready.
const KindNotifyRecording = "calls.recording"

// EventRecordingUpdated goes to calls:<meeting id> when a recording changes.
const EventRecordingUpdated = "calls.recording_updated"

const recordingPrefix = "calls_recording"

type recordingSettings struct {
	enabled    bool
	retention  time.Duration // 0 = keep until deleted
	s3Endpoint string        // where the recorder reaches S3, when not S3_ENDPOINT
	layout     string
}

func readRecordingSettings() recordingSettings {
	rs := recordingSettings{
		enabled:    config.Bool("CALLS_RECORDING", false),
		retention:  config.Duration("CALLS_RECORDING_RETENTION", 90*24*time.Hour),
		s3Endpoint: config.String("CALLS_RECORDING_S3_ENDPOINT", ""),
		layout:     config.String("CALLS_RECORDING_LAYOUT", "speaker"),
	}
	if rs.retention < 0 {
		config.Problem("CALLS_RECORDING_RETENTION must be 0 (keep) or a positive duration (got %s)", rs.retention)
		rs.retention = 0
	}
	switch rs.layout {
	case "speaker", "grid", "single-speaker":
	default:
		config.Problem("CALLS_RECORDING_LAYOUT must be speaker, grid or single-speaker (got %q)", rs.layout)
		rs.layout = "speaker"
	}
	return rs
}

// recordingAvailable: recording is switched on, the media server can
// record, and the files have an S3 bucket to go to.
func (s *Service) recordingAvailable() bool {
	if !s.rec.enabled || s.media == nil || s.d.Cfg == nil || s.d.Cfg.S3Bucket == "" {
		return false
	}
	_, ok := s.media.(Recorder)
	return ok
}

// Recording is one recording of a meeting's call.
type Recording struct {
	ID              uuid.UUID  `json:"id"`
	MeetingID       uuid.UUID  `json:"meeting_id"`
	MeetingTitle    string     `json:"meeting_title"`
	SessionID       *uuid.UUID `json:"session_id"`
	Status          string     `json:"status"` // starting | recording | processing | ready | failed
	StartedBy       *UserRef   `json:"started_by"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`
	DurationSeconds int        `json:"duration_seconds"`
	SizeBytes       *int64     `json:"size_bytes"`
	ExpiresAt       *time.Time `json:"expires_at"`
	Error           *string    `json:"error"`
	CanDelete       bool       `json:"can_delete"`
	URL             *string    `json:"url"`          // playback (ready recordings, one read)
	DownloadURL     *string    `json:"download_url"` // saves as a file
}

// recordsVisibleSQL is the records rule as a SQL condition on m (the
// meeting) for user $uid and the session in sessionExpr: hosts and co-hosts
// always; people in that session; invitees when records_access is invited
// or org; the workspace's members when it is org.
func recordsVisibleSQL(uid, sessionExpr string) string {
	return `(m.host_id = ` + uid + `
	  OR EXISTS (SELECT 1 FROM calls_invitees ri WHERE ri.meeting_id = m.id AND ri.user_id = ` + uid + `
	             AND (ri.role = 'cohost' OR m.records_access IN ('invited', 'org')))
	  OR (` + sessionExpr + ` IS NOT NULL AND EXISTS (SELECT 1 FROM calls_participants rp WHERE rp.session_id = ` + sessionExpr + ` AND rp.user_id = ` + uid + `))
	  OR (m.records_access = 'org' AND m.org_id IS NOT NULL
	      AND EXISTS (SELECT 1 FROM org_members rom WHERE rom.org_id = m.org_id AND rom.user_id = ` + uid + `)))`
}

// isMeetingHost: the host or a co-host.
func isMeetingHost(ctx context.Context, q db.Querier, meetingID, hostID, userID uuid.UUID) (bool, error) {
	if hostID == userID {
		return true, nil
	}
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM calls_invitees WHERE meeting_id = $1 AND user_id = $2 AND role = 'cohost')`, meetingID, userID).Scan(&ok)
	return ok, err
}

const recordingCols = `r.id, r.meeting_id, m.title, r.session_id, r.status, r.started_by, su.display_name, su.avatar_url, su.status,
	r.started_at, r.ended_at, r.duration_ms, r.size_bytes, r.expires_at, r.error, r.storage_key, m.host_id`

type recordingRow struct {
	Recording
	key    string
	hostID uuid.UUID
}

func scanRecordings(rows pgx.Rows) ([]recordingRow, error) {
	defer rows.Close()
	out := []recordingRow{}
	for rows.Next() {
		var r recordingRow
		var by *uuid.UUID
		var byName, byAvatar, byStatus *string
		var ms *int64
		if err := rows.Scan(&r.ID, &r.MeetingID, &r.MeetingTitle, &r.SessionID, &r.Status, &by, &byName, &byAvatar, &byStatus,
			&r.StartedAt, &r.EndedAt, &ms, &r.SizeBytes, &r.ExpiresAt, &r.Error, &r.key, &r.hostID); err != nil {
			return nil, err
		}
		if by != nil {
			u := userRef(*by, deref(byName), byAvatar, deref(byStatus))
			r.StartedBy = &u
		}
		if ms != nil {
			r.DurationSeconds = int(*ms / 1000)
		} else if r.EndedAt != nil {
			r.DurationSeconds = int(r.EndedAt.Sub(r.StartedAt).Seconds())
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// visibleRecordings lists recordings userID may open (where: more conditions, args from $2).
func visibleRecordings(ctx context.Context, q db.Querier, userID uuid.UUID, where string, args ...any) ([]recordingRow, error) {
	rows, err := q.Query(ctx, `SELECT `+recordingCols+`
		FROM calls_recordings r JOIN calls_meetings m ON m.id = r.meeting_id LEFT JOIN users su ON su.id = r.started_by
		WHERE `+recordsVisibleSQL("$1", "r.session_id")+` AND `+where+`
		ORDER BY r.started_at DESC, r.id DESC LIMIT 200`, append([]any{userID}, args...)...)
	if err != nil {
		return nil, err
	}
	return scanRecordings(rows)
}

func recordingNow(ctx context.Context, q db.Querier, meetingID uuid.UUID) (bool, error) {
	var on bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM calls_recordings WHERE meeting_id = $1 AND status IN ('starting', 'recording'))`, meetingID).Scan(&on)
	return on, err
}

func (s *Service) finishRecording(ctx context.Context, r *recordingRow, userID uuid.UUID, withURLs bool) error {
	host, err := isMeetingHost(ctx, s.d.Pool, r.MeetingID, r.hostID, userID)
	if err != nil {
		return err
	}
	r.CanDelete = host || (r.StartedBy != nil && r.StartedBy.ID == userID)
	if !withURLs || r.Status != "ready" || s.d.Storage == nil {
		return nil
	}
	play, err := s.d.Storage.PresignGetWith(ctx, r.key, time.Hour, storage.GetOptions{ContentType: "video/mp4", Disposition: "inline"})
	if err != nil {
		return err
	}
	name := strings.TrimSpace(r.MeetingTitle)
	if name == "" {
		name = "Recording"
	}
	name = strings.NewReplacer("/", "-", "\\", "-", "\"", "", "\n", " ").Replace(name) + " " + r.StartedAt.UTC().Format("2006-01-02") + ".mp4"
	dl, err := s.d.Storage.PresignGetWith(ctx, r.key, time.Hour, storage.GetOptions{ContentType: "video/mp4",
		Disposition: mime.FormatMediaType("attachment", map[string]string{"filename": name})})
	if err != nil {
		return err
	}
	r.URL, r.DownloadURL = &play, &dl
	return nil
}

// ---- host actions --------------------------------------------------------------

var (
	errRecordingUnavailable = httpx.NewError(http.StatusServiceUnavailable, "calls_recording_unavailable", "Recording isn't set up on this server.")
	errAlreadyRecording     = httpx.NewError(http.StatusConflict, "calls_already_recording", "This call is already being recorded.")
	errNotRecording         = httpx.NewError(http.StatusConflict, "calls_not_recording", "This call isn't being recorded.")
	errRecordingNotFound    = httpx.NotFound("Recording not found.")
)

// startRecording: POST /calls/meetings/{id}/recording (hosts, live) → Recording.
func (s *Service) startRecording(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, _, err := s.liveMeetingAsHost(r, me)
	if err != nil {
		return err
	}
	if !s.recordingAvailable() {
		return errRecordingUnavailable
	}
	org, err := orgSettings(ctx, s.d.Pool, m.OrgID)
	if err != nil {
		return err
	}
	if !org.Recording {
		return httpx.NewError(http.StatusForbidden, "calls_recording_off", "Recording is turned off for this "+orgs.Noun()+"'s calls.")
	}
	id := uuid.New()
	key := fmt.Sprintf("%s/%s/%s.mp4", recordingPrefix, m.ID, id)
	_, err = s.d.Pool.Exec(ctx, `INSERT INTO calls_recordings (id, meeting_id, session_id, started_by, storage_key) VALUES ($1, $2, $3, $4, $5)`,
		id, m.ID, m.LiveSession, me.UserID, key)
	if db.IsUniqueViolation(err, "calls_recordings_active_idx") {
		return errAlreadyRecording
	}
	if err != nil {
		return err
	}
	cfg := s.d.Cfg
	endpoint := cfg.S3Endpoint
	if s.rec.s3Endpoint != "" {
		endpoint = s.rec.s3Endpoint
	}
	dctx, cancel := detached(ctx)
	defer cancel()
	info, err := s.media.(Recorder).StartRecording(dctx, m.ID.String(), RecordingOutput{Filepath: key, Layout: s.rec.layout, S3: S3Target{
		AccessKey: cfg.S3AccessKeyID, Secret: cfg.S3SecretKey, Region: cfg.S3Region, Endpoint: endpoint, Bucket: cfg.S3Bucket, ForcePathStyle: cfg.S3ForcePathStyle,
	}})
	if err != nil {
		_, _ = s.d.Pool.Exec(dctx, `DELETE FROM calls_recordings WHERE id = $1`, id)
		return s.mediaErr(ctx, err)
	}
	status := "starting"
	if info.Status == RecActive {
		status = "recording"
	}
	if _, err := s.d.Pool.Exec(dctx, `UPDATE calls_recordings SET egress_id = $2, status = $3, updated_at = now() WHERE id = $1`, id, info.ID, status); err != nil {
		return err
	}
	s.recordingChanged(ctx, m.ID)
	return s.respondRecording(w, ctx, id, me.UserID, http.StatusCreated)
}

// stopRecording: DELETE /calls/meetings/{id}/recording (hosts). The file
// is ready a little later (egress_ended).
func (s *Service) stopRecording(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, _, err := s.meetingAsHost(r, me)
	if err != nil {
		return err
	}
	rec, ok := s.media.(Recorder)
	if !ok {
		return errRecordingUnavailable
	}
	var id uuid.UUID
	var egress *string
	err = s.d.Pool.QueryRow(ctx, `SELECT id, egress_id FROM calls_recordings WHERE meeting_id = $1 AND status IN ('starting', 'recording')`, m.ID).Scan(&id, &egress)
	if db.IsNoRows(err) {
		return errNotRecording
	}
	if err != nil {
		return err
	}
	dctx, cancel := detached(ctx)
	defer cancel()
	if egress != nil {
		if _, err := rec.StopRecording(dctx, *egress); err != nil && err != errMediaNotFound {
			return s.mediaErr(ctx, err)
		}
	}
	if _, err := s.d.Pool.Exec(dctx, `UPDATE calls_recordings SET status = CASE WHEN egress_id IS NULL THEN 'failed' ELSE 'processing' END,
		error = CASE WHEN egress_id IS NULL THEN 'The recording never started.' END, stopped_by = $2, ended_at = now(), updated_at = now()
		WHERE id = $1 AND status IN ('starting', 'recording')`, id, me.UserID); err != nil {
		return err
	}
	s.recordingChanged(ctx, m.ID)
	return s.respondRecording(w, ctx, id, me.UserID, http.StatusOK)
}

func (s *Service) respondRecording(w http.ResponseWriter, ctx context.Context, id, userID uuid.UUID, status int) error {
	list, err := visibleRecordings(ctx, s.d.Pool, userID, `r.id = $2`, id)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return errRecordingNotFound
	}
	if err := s.finishRecording(ctx, &list[0], userID, true); err != nil {
		return err
	}
	httpx.JSON(w, status, list[0].Recording)
	return nil
}

// recordingChanged updates the room (indicator, consent) and the meeting page.
func (s *Service) recordingChanged(ctx context.Context, meetingID uuid.UUID) {
	s.syncRoom(ctx, meetingID)
	s.publish(ctx, meetingTopic(meetingID), EventRecordingUpdated, map[string]any{"meeting_id": meetingID})
}

// ---- reads ------------------------------------------------------------------

// listMeetingRecordings: GET /calls/meetings/{id}/recordings → recordings of
// this meeting the caller may open.
func (s *Service) listMeetingRecordings(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return errMeetingNotFound
	}
	list, err := visibleRecordings(ctx, s.d.Pool, me.UserID, `r.meeting_id = $2`, id)
	if err != nil {
		return err
	}
	out := make([]Recording, 0, len(list))
	for i := range list {
		if err := s.finishRecording(ctx, &list[i], me.UserID, false); err != nil {
			return err
		}
		out = append(out, list[i].Recording)
	}
	httpx.OK(w, httpx.Page[Recording]{Items: out})
	return nil
}

func recordingParam(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "recordingID"))
	if err != nil {
		return uuid.Nil, errRecordingNotFound
	}
	return id, nil
}

// getRecording: GET /calls/recordings/{recordingID} → Recording with
// playback and download URLs (one hour) once ready. 404 for anyone the
// records rule leaves out.
func (s *Service) getRecording(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	id, err := recordingParam(r)
	if err != nil {
		return err
	}
	return s.respondRecording(w, r.Context(), id, me.UserID, http.StatusOK)
}

// deleteRecording: DELETE /calls/recordings/{recordingID} (hosts, co-hosts
// or whoever started it). The file goes first, then the row.
func (s *Service) deleteRecording(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := recordingParam(r)
	if err != nil {
		return err
	}
	list, err := visibleRecordings(ctx, s.d.Pool, me.UserID, `r.id = $2`, id)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return errRecordingNotFound
	}
	rec := &list[0]
	if err := s.finishRecording(ctx, rec, me.UserID, false); err != nil {
		return err
	}
	if !rec.CanDelete {
		return httpx.Forbidden("Only the meeting's hosts or whoever recorded it can delete a recording.")
	}
	if rec.Status == "starting" || rec.Status == "recording" {
		return httpx.Conflict("Stop the recording first.")
	}
	if err := s.removeRecording(ctx, id, rec.key); err != nil {
		return err
	}
	s.publish(ctx, meetingTopic(rec.MeetingID), EventRecordingUpdated, map[string]any{"meeting_id": rec.MeetingID})
	httpx.NoContent(w)
	return nil
}

func (s *Service) removeRecording(ctx context.Context, id uuid.UUID, key string) error {
	if s.d.Storage != nil && key != "" {
		if err := s.d.Storage.Delete(ctx, key); err != nil {
			return err
		}
	}
	_, err := s.d.Pool.Exec(ctx, `DELETE FROM calls_recordings WHERE id = $1`, id)
	return err
}

// ---- webhooks and the sweep -------------------------------------------------------

// applyEgress records an egress_* event inside the webhook transaction.
// Unknown egress ids (not ours) are ignored. It returns the meeting and
// whether the recording just became ready (to tell whoever started it).
func (s *Service) applyEgress(ctx context.Context, tx pgx.Tx, info RecordingInfo) (meetingID uuid.UUID, ready bool, err error) {
	if info.ID == "" {
		return uuid.Nil, false, nil
	}
	var status string
	err = tx.QueryRow(ctx, `SELECT meeting_id, status FROM calls_recordings WHERE egress_id = $1 FOR UPDATE`, info.ID).Scan(&meetingID, &status)
	if db.IsNoRows(err) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	if status == "ready" || status == "failed" {
		return meetingID, false, nil // final already
	}
	switch info.Status {
	case RecActive:
		_, err = tx.Exec(ctx, `UPDATE calls_recordings SET status = 'recording', updated_at = now() WHERE egress_id = $1 AND status = 'starting'`, info.ID)
	case RecEnding:
		_, err = tx.Exec(ctx, `UPDATE calls_recordings SET status = 'processing', ended_at = COALESCE(ended_at, now()), updated_at = now() WHERE egress_id = $1`, info.ID)
	case RecComplete:
		if info.SizeBytes <= 0 {
			_, err = tx.Exec(ctx, `UPDATE calls_recordings SET status = 'failed', error = 'The recording is empty.', ended_at = COALESCE(ended_at, now()), updated_at = now() WHERE egress_id = $1`, info.ID)
			return meetingID, false, err
		}
		var expires *time.Time
		if s.rec.retention > 0 {
			t := time.Now().Add(s.rec.retention).UTC()
			expires = &t
		}
		var ended *time.Time
		if !info.EndedAt.IsZero() {
			ended = &info.EndedAt
		}
		_, err = tx.Exec(ctx, `UPDATE calls_recordings SET status = 'ready', size_bytes = $2, duration_ms = $3,
			ended_at = COALESCE($4, ended_at, now()), expires_at = $5, error = NULL, updated_at = now() WHERE egress_id = $1`,
			info.ID, info.SizeBytes, info.DurationMs, ended, expires)
		return meetingID, err == nil, err
	case RecFailed:
		msg := strings.TrimSpace(info.Error)
		if msg == "" {
			msg = "The recording failed."
		}
		if len(msg) > 500 {
			msg = msg[:500]
		}
		_, err = tx.Exec(ctx, `UPDATE calls_recordings SET status = 'failed', error = $2, ended_at = COALESCE(ended_at, now()), updated_at = now() WHERE egress_id = $1`, info.ID, msg)
	}
	return meetingID, false, err
}

// afterEgress tells the room and, when ready, whoever started it.
func (s *Service) afterEgress(ctx context.Context, meetingID uuid.UUID, egressID string, ready bool) {
	s.recordingChanged(ctx, meetingID)
	if !ready {
		return
	}
	var id uuid.UUID
	var by *uuid.UUID
	var title string
	if err := s.d.Pool.QueryRow(ctx, `SELECT r.id, COALESCE(r.started_by, m.host_id), m.title FROM calls_recordings r JOIN calls_meetings m ON m.id = r.meeting_id
		WHERE r.egress_id = $1`, egressID).Scan(&id, &by, &title); err != nil || by == nil {
		return
	}
	if title == "" {
		title = "your call"
	}
	s.send(ctx, notify.Notification{UserID: *by, Kind: KindNotifyRecording, Title: "Recording ready",
		Body: "The recording of " + title + " is ready to watch.", Link: "/calls/recordings/" + id.String(),
		Data: map[string]any{"meeting_id": meetingID, "recording_id": id}})
}

// sweepRecordings (job): deletes recordings past their retention and fails
// recordings that never started or never finished (missed webhooks).
func (s *Service) sweepRecordings(ctx context.Context) error {
	if _, err := s.d.Pool.Exec(ctx, `UPDATE calls_recordings SET status = 'failed', error = 'The recording didn''t finish.', updated_at = now()
		WHERE (status = 'starting' AND egress_id IS NULL AND created_at < now() - interval '5 minutes')
		   OR (status IN ('starting', 'recording', 'processing') AND created_at < now() - interval '30 hours')`); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, storage_key, meeting_id FROM calls_recordings WHERE expires_at < now() ORDER BY expires_at LIMIT 100`)
	if err != nil {
		return err
	}
	type due struct {
		ID        uuid.UUID
		Key       string
		MeetingID uuid.UUID
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[due])
	if err != nil {
		return err
	}
	for _, d := range list {
		if err := s.removeRecording(ctx, d.ID, d.Key); err != nil {
			slog.WarnContext(ctx, "calls: deleting an expired recording failed", "recording_id", d.ID, "err", err)
			continue
		}
		s.publish(ctx, meetingTopic(d.MeetingID), EventRecordingUpdated, map[string]any{"meeting_id": d.MeetingID})
	}
	return nil
}
