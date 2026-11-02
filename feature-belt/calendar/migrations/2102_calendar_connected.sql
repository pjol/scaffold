-- calendar 1.2.0: connected calendars (Google Calendar, Outlook) through the
-- person's own connections. Platform events are never stored here: only the
-- person's choices per connected calendar, the platform copy of a booking
-- (a reference), and rooms made for connected events (references).

-- Colour, visibility and "counts for availability" per connected calendar.
-- NULL = the default (platform order colour; shown; primary counts).
CREATE TABLE calendar_connected_calendars (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  connection_id text NOT NULL,
  calendar_id text NOT NULL,
  color text,
  hidden boolean,
  busy boolean,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, connection_id, calendar_id)
);

-- Rooms the wired conferencing made for events on connected calendars
-- (a Zoom meeting or the app's own call written into a Google event), so
-- moving the event moves the room and deleting it closes the room.
CREATE TABLE calendar_connected_conferences (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  connection_id text NOT NULL,
  calendar_id text NOT NULL,
  event_id text NOT NULL,
  conference_id text NOT NULL,
  provider text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, connection_id, calendar_id, event_id)
);

-- Booking links: the connected calendar bookings are also written to, and
-- which meeting provider a video booking uses.
ALTER TABLE calendar_booking_links
  ADD COLUMN write_connection_id text,
  ADD COLUMN write_calendar_id text,
  ADD COLUMN conference_provider text NOT NULL DEFAULT 'app',
  ADD COLUMN conference_connection_id text,
  ADD CONSTRAINT calendar_booking_links_write_target CHECK ((write_connection_id IS NULL) = (write_calendar_id IS NULL));

-- Bookings: the platform copy of the host's event, when the link writes to
-- a connected calendar.
ALTER TABLE calendar_bookings
  ADD COLUMN ext_connection_id text,
  ADD COLUMN ext_calendar_id text,
  ADD COLUMN ext_event_id text,
  ADD COLUMN conference_provider text;
CREATE INDEX calendar_bookings_ext_idx ON calendar_bookings (host_id) WHERE ext_event_id IS NOT NULL AND status = 'confirmed';

-- Outbox: changes to a booking's platform copy (move, delete) after commit.
ALTER TABLE calendar_outbox DROP CONSTRAINT calendar_outbox_kind_check;
ALTER TABLE calendar_outbox ADD CONSTRAINT calendar_outbox_kind_check
  CHECK (kind IN ('mail', 'notify', 'conference_cancel', 'connected_event'));
