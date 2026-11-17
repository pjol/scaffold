package calendar

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/mail"
)

// agendaHour is when the morning agenda goes out, in the person's zone. A
// run that misses the window (the app was down) skips that day rather than
// mail a stale agenda in the afternoon.
const (
	agendaHour     = 6
	agendaLastHour = 10
)

// sendAgendas emails people who opted in their day's events, once per
// local day (claimed by setting agenda_sent_on before anything is built).
// Days with nothing on them send nothing.
func (s *Service) sendAgendas(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT st.user_id, st.timezone, u.email, (now() AT TIME ZONE st.timezone)::date::text
		FROM calendar_settings st JOIN users u ON u.id = st.user_id
		WHERE st.agenda_email AND st.timezone IS NOT NULL AND u.status = 'active' AND u.email <> ''
		  AND (st.agenda_sent_on IS NULL OR st.agenda_sent_on < (now() AT TIME ZONE st.timezone)::date)
		  AND extract(hour FROM now() AT TIME ZONE st.timezone) >= $1
		  AND extract(hour FROM now() AT TIME ZONE st.timezone) < $2
		LIMIT 200`, agendaHour, agendaLastHour)
	if err != nil {
		return err
	}
	type due struct {
		user      uuid.UUID
		tz, email string
		date      string
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (due, error) {
		var d due
		return d, row.Scan(&d.user, &d.tz, &d.email, &d.date)
	})
	if err != nil {
		return err
	}
	sent := 0
	for _, d := range list {
		tag, err := s.d.Pool.Exec(ctx, `UPDATE calendar_settings SET agenda_sent_on = $2::date
			WHERE user_id = $1 AND (agenda_sent_on IS NULL OR agenda_sent_on < $2::date)`, d.user, d.date)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		loc := zoneOr(d.tz, time.UTC)
		day, err := time.ParseInLocation(time.DateOnly, d.date, loc)
		if err != nil {
			continue
		}
		occ, err := s.occurrences(ctx, s.d.Pool, d.user, day.UTC(), day.AddDate(0, 0, 1).UTC(), 200)
		if err != nil {
			slog.WarnContext(ctx, "calendar: agenda failed", "err", err)
			continue
		}
		msg, ok := s.agendaMessage(d.email, day, loc, occ)
		if !ok {
			continue
		}
		if err := enqueueMail(ctx, s.d.Pool, msg); err != nil {
			return err
		}
		sent++
	}
	if sent > 0 {
		kickOutbox()
	}
	return nil
}

// agendaMessage words a day's events (declined ones left out); false when
// there's nothing to send.
func (s *Service) agendaMessage(to string, day time.Time, loc *time.Location, occ []Occurrence) (mail.Message, bool) {
	var lines []string
	for _, o := range occ {
		if o.MyResponse != nil && *o.MyResponse == ResponseDeclined {
			continue
		}
		when := "All day"
		if !o.AllDay {
			when = o.StartsAt.In(loc).Format("3:04 PM") + " – " + o.EndsAt.In(loc).Format("3:04 PM")
		}
		line := when + "  " + o.Title
		if o.Location != "" {
			line += " (" + o.Location + ")"
		}
		if o.MyResponse != nil && *o.MyResponse == ResponseNeedsAction {
			line += " · not answered"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return mail.Message{}, false
	}
	n := strconv.Itoa(len(lines)) + " events"
	if len(lines) == 1 {
		n = "1 event"
	}
	date := day.Format("Mon, Jan 2")
	paras := []string{"Your day, " + date + " (" + loc.String() + "):", strings.Join(lines, "\n")}
	var links []mailLink
	if u := s.webURL("/calendar?view=day&date=" + day.Format(time.DateOnly)); u != "" {
		links = append(links, mailLink{"Open your calendar", u})
	}
	if u := s.webURL("/settings/calendars"); u != "" {
		links = append(links, mailLink{"Stop these emails", u})
	}
	text, html := s.mailBody(paras, links)
	return mail.Message{To: to, Subject: "Your day: " + date + " · " + n, Text: text, HTML: html}, true
}
