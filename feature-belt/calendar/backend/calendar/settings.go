package calendar

import (
	"encoding/json"
	"net/http"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	st, err := s.loadSettings(r.Context(), s.d.Pool, id.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, st)
	return nil
}

// putSettings saves the fields sent: timezone, week_start (0 Sunday,
// 1 Monday, 6 Saturday), working_hours, default_reminders.
func (s *Service) putSettings(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var in struct {
		Timezone         *string          `json:"timezone"`
		WeekStart        *int             `json:"week_start"`
		WorkingHours     *[]HoursInterval `json:"working_hours"`
		DefaultReminders *[]int           `json:"default_reminders"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	st, err := s.loadSettings(r.Context(), s.d.Pool, id.UserID)
	if err != nil {
		return err
	}
	tz := st.Timezone
	if !st.TimezoneSet {
		tz = ""
	}
	if in.Timezone != nil {
		if !validZone(*in.Timezone) {
			return httpx.BadRequest("Pick a valid time zone.")
		}
		tz = *in.Timezone
	}
	if in.WeekStart != nil {
		if *in.WeekStart != 0 && *in.WeekStart != 1 && *in.WeekStart != 6 {
			return httpx.BadRequest("Weeks start on Sunday, Monday or Saturday.")
		}
		st.WeekStart = *in.WeekStart
	}
	if in.WorkingHours != nil {
		h, err := validateHours(*in.WorkingHours)
		if err != nil {
			return err
		}
		st.WorkingHours = h
	}
	if in.DefaultReminders != nil {
		rem, err := validateReminders(*in.DefaultReminders)
		if err != nil {
			return err
		}
		st.DefaultReminders = rem
	}
	hours, _ := json.Marshal(st.WorkingHours)
	var tzArg *string
	if tz != "" {
		tzArg = &tz
	}
	if _, err := s.d.Pool.Exec(r.Context(), `
		INSERT INTO calendar_settings (user_id, timezone, week_start, working_hours, default_reminders)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET timezone = EXCLUDED.timezone, week_start = EXCLUDED.week_start,
		  working_hours = EXCLUDED.working_hours, default_reminders = EXCLUDED.default_reminders, updated_at = now()`,
		id.UserID, tzArg, st.WeekStart, hours, toInt32(st.DefaultReminders)); err != nil {
		return err
	}
	out, err := s.loadSettings(r.Context(), s.d.Pool, id.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}
