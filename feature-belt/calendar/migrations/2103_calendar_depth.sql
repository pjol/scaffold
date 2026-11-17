-- calendar 1.3.0: several calendars per person and team calendars shared
-- through access, out of office and focus time, RSVP notes and proposed
-- times, rooms and resources, team booking links (round robin and
-- collective), booking link depth (single use, redirect, reminders to
-- bookers, guests), working locations, a secondary zone and world clocks.

-- Calendars. Each person gets a primary calendar on first use; more are
-- created in a workspace so they can be shared with its teams. Each is an
-- access resource of kind calendar.calendar: view = free/busy only,
-- comment = all event details, edit = make changes to events, manage =
-- make changes and manage sharing.
CREATE TABLE calendar_calendars (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id uuid REFERENCES users(id),               -- NULL: a team calendar whose creator left (admins manage)
  org_id uuid REFERENCES orgs(id) ON DELETE SET NULL,
  team_id uuid REFERENCES access_teams(id) ON DELETE SET NULL,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  color text NOT NULL DEFAULT 'primary' CHECK (color IN ('primary', 'accent', 'success', 'warning', 'danger', 'info', 'neutral')),
  is_primary boolean NOT NULL DEFAULT false,
  counts_busy boolean NOT NULL DEFAULT true,        -- the owner's events here count as busy time
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (NOT is_primary OR (owner_id IS NOT NULL AND team_id IS NULL))
);
CREATE UNIQUE INDEX calendar_calendars_primary_key ON calendar_calendars (owner_id) WHERE is_primary;
CREATE INDEX calendar_calendars_owner_idx ON calendar_calendars (owner_id, created_at);
CREATE INDEX calendar_calendars_org_idx ON calendar_calendars (org_id) WHERE org_id IS NOT NULL;

-- Each person's view of a calendar: its colour for them, hidden, and
-- whether a calendar shared with them is in their list.
CREATE TABLE calendar_calendar_prefs (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  calendar_id uuid NOT NULL REFERENCES calendar_calendars(id) ON DELETE CASCADE,
  color text CHECK (color IS NULL OR color IN ('primary', 'accent', 'success', 'warning', 'danger', 'info', 'neutral')),
  hidden boolean NOT NULL DEFAULT false,
  added boolean NOT NULL DEFAULT true,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, calendar_id)
);
CREATE INDEX calendar_calendar_prefs_calendar_idx ON calendar_calendar_prefs (calendar_id);

-- Events: their calendar (NULL = the owner's primary, filled when it is
-- made), and the type: out of office and focus time decline invitations.
ALTER TABLE calendar_events
  ADD COLUMN calendar_id uuid REFERENCES calendar_calendars(id) ON DELETE CASCADE,
  ADD COLUMN event_type text NOT NULL DEFAULT 'default' CHECK (event_type IN ('default', 'out_of_office', 'focus')),
  ADD COLUMN auto_decline text NOT NULL DEFAULT 'none' CHECK (auto_decline IN ('none', 'new', 'all')),
  ADD COLUMN decline_message text NOT NULL DEFAULT '';
CREATE INDEX calendar_events_calendar_idx ON calendar_events (calendar_id, starts_at) WHERE recurring_event_id IS NULL;
CREATE INDEX calendar_events_unfiled_idx ON calendar_events (owner_id) WHERE calendar_id IS NULL;
CREATE INDEX calendar_events_blocks_idx ON calendar_events (owner_id, starts_at) WHERE event_type <> 'default' AND recurring_event_id IS NULL;

-- Attendees: a note with the reply, and a proposed new time.
ALTER TABLE calendar_attendees
  ADD COLUMN note text NOT NULL DEFAULT '',
  ADD COLUMN proposed_start timestamptz,
  ADD COLUMN proposed_end timestamptz,
  ADD COLUMN proposed_at timestamptz,
  ADD COLUMN auto_declined boolean NOT NULL DEFAULT false,
  ADD CONSTRAINT calendar_attendees_proposal CHECK ((proposed_start IS NULL) = (proposed_end IS NULL)
    AND (proposed_end IS NULL OR proposed_end > proposed_start));

-- Rooms and resources (a projector, a car): workspace-level, access
-- resources of kind calendar.room (view = see free/busy, edit = book,
-- manage = change the room). An event books a room for every occurrence.
CREATE TABLE calendar_rooms (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  kind text NOT NULL DEFAULT 'room' CHECK (kind IN ('room', 'resource')),
  name text NOT NULL,
  building text NOT NULL DEFAULT '',
  floor text NOT NULL DEFAULT '',
  capacity integer CHECK (capacity IS NULL OR capacity BETWEEN 1 AND 10000),
  features text[] NOT NULL DEFAULT '{}',
  description text NOT NULL DEFAULT '',
  active boolean NOT NULL DEFAULT true,
  created_by uuid REFERENCES users(id) ON DELETE SET NULL,
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calendar_rooms_org_idx ON calendar_rooms (org_id, lower(name), id) WHERE deleted_at IS NULL;

CREATE TABLE calendar_event_rooms (
  event_id uuid NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
  room_id uuid NOT NULL REFERENCES calendar_rooms(id) ON DELETE CASCADE,
  added_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (event_id, room_id)
);
CREATE INDEX calendar_event_rooms_room_idx ON calendar_event_rooms (room_id);

-- Booking links: team links (round robin, collective) owned by an access
-- team, single use, redirect after booking, reminder emails to bookers,
-- guests the booker adds.
ALTER TABLE calendar_booking_links
  ADD COLUMN kind text NOT NULL DEFAULT 'personal' CHECK (kind IN ('personal', 'round_robin', 'collective')),
  ADD COLUMN team_id uuid REFERENCES access_teams(id) ON DELETE CASCADE,
  ADD COLUMN single_use boolean NOT NULL DEFAULT false,
  ADD COLUMN used_at timestamptz,
  ADD COLUMN redirect_url text NOT NULL DEFAULT '',
  ADD COLUMN booker_reminders integer[] NOT NULL DEFAULT '{}',
  ADD COLUMN allow_guests boolean NOT NULL DEFAULT false,
  ADD CONSTRAINT calendar_booking_links_team CHECK (kind = 'personal' OR (team_id IS NOT NULL AND org_id IS NOT NULL));
CREATE INDEX calendar_booking_links_team_idx ON calendar_booking_links (team_id, created_at DESC, id DESC) WHERE deleted_at IS NULL AND team_id IS NOT NULL;

-- A team link's hosts: priority (4 highest) and weight decide round robin;
-- weekly_hours + timezone (both or neither) replace the link's hours.
CREATE TABLE calendar_link_hosts (
  link_id uuid NOT NULL REFERENCES calendar_booking_links(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  priority smallint NOT NULL DEFAULT 2 CHECK (priority BETWEEN 0 AND 4),
  weight smallint NOT NULL DEFAULT 100 CHECK (weight BETWEEN 1 AND 1000),
  weekly_hours jsonb,
  timezone text,
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (link_id, user_id),
  CHECK ((weekly_hours IS NULL) = (timezone IS NULL))
);
CREATE INDEX calendar_link_hosts_user_idx ON calendar_link_hosts (user_id);

-- Collective bookings: the other hosts (host_id on the booking organizes).
CREATE TABLE calendar_booking_hosts (
  booking_id uuid NOT NULL REFERENCES calendar_bookings(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (booking_id, user_id)
);
CREATE INDEX calendar_booking_hosts_user_idx ON calendar_booking_hosts (user_id);

ALTER TABLE calendar_bookings
  ADD COLUMN guests text[] NOT NULL DEFAULT '{}',
  ADD COLUMN assignment text CHECK (assignment IN ('round_robin', 'collective', 'reassigned'));

-- Reminder emails to bookers: at most once per booking per offset.
CREATE TABLE calendar_booking_reminders_sent (
  booking_id uuid NOT NULL REFERENCES calendar_bookings(id) ON DELETE CASCADE,
  offset_minutes integer NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (booking_id, offset_minutes)
);

-- Focus time that started, handed to the focus hook once per occurrence.
CREATE TABLE calendar_focus_signals (
  event_id uuid NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
  occurrence_start timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (event_id, occurrence_start)
);
CREATE INDEX calendar_focus_signals_created_idx ON calendar_focus_signals (created_at);

-- Settings: where someone works each weekday, a second zone in the day
-- and week views, world clocks, and the opt-in morning agenda email.
ALTER TABLE calendar_settings
  ADD COLUMN work_locations jsonb NOT NULL DEFAULT '[]',   -- [{"day":1,"type":"office","label":"HQ"}]
  ADD COLUMN secondary_timezone text,
  ADD COLUMN world_clocks text[] NOT NULL DEFAULT '{}',
  ADD COLUMN agenda_email boolean NOT NULL DEFAULT false,  -- opt-in: the day's events by email at 6 AM local
  ADD COLUMN agenda_sent_on date;
CREATE INDEX calendar_settings_agenda_idx ON calendar_settings (user_id) WHERE agenda_email;
