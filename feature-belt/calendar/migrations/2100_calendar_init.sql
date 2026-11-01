-- calendar: personal calendars (events, recurrence, attendees, reminders),
-- external iCal subscriptions for busy time, a personal iCal feed, and
-- booking links with race-safe bookings.

-- btree_gist lets an exclusion constraint combine uuid equality with range
-- overlap; pg_trgm backs title search. Both are trusted extensions.
CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- One row per user once they change a setting. Missing row = defaults.
CREATE TABLE calendar_settings (
  user_id uuid PRIMARY KEY REFERENCES users(id),
  timezone text,                                    -- IANA zone; NULL = not chosen yet (UTC)
  week_start smallint NOT NULL DEFAULT 1 CHECK (week_start IN (0, 1, 6)),
  working_hours jsonb NOT NULL DEFAULT '[]',        -- [{"day":1,"start":540,"end":1020}], minutes in the zone
  default_reminders integer[] NOT NULL DEFAULT '{10}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- Events. A row is a single event, a series master (rrule set), or an
-- exception to a series (recurring_event_id + original_start): a moved or
-- edited occurrence, or a cancelled one (status = 'cancelled').
CREATE TABLE calendar_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id uuid NOT NULL REFERENCES users(id),
  recurring_event_id uuid REFERENCES calendar_events(id) ON DELETE CASCADE,
  original_start timestamptz,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  location text NOT NULL DEFAULT '',
  conference_url text,
  conference_id text,
  starts_at timestamptz NOT NULL,
  ends_at timestamptz NOT NULL,
  timezone text NOT NULL,
  all_day boolean NOT NULL DEFAULT false,
  rrule text,
  series_until timestamptz,                         -- masters: end of the last occurrence; NULL = endless
  status text NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed', 'cancelled')),
  visibility text NOT NULL DEFAULT 'busy' CHECK (visibility IN ('busy', 'details')),
  show_as text NOT NULL DEFAULT 'busy' CHECK (show_as IN ('busy', 'free')),
  color text NOT NULL DEFAULT 'primary' CHECK (color IN ('primary', 'accent', 'success', 'warning', 'danger', 'info', 'neutral')),
  reminders integer[] NOT NULL DEFAULT '{}',
  sequence integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (ends_at > starts_at),
  CHECK ((recurring_event_id IS NULL) = (original_start IS NULL)),
  CHECK (rrule IS NULL OR recurring_event_id IS NULL)
);
CREATE UNIQUE INDEX calendar_events_exception_key ON calendar_events (recurring_event_id, original_start) WHERE recurring_event_id IS NOT NULL;
CREATE INDEX calendar_events_single_idx ON calendar_events (owner_id, starts_at) WHERE recurring_event_id IS NULL AND rrule IS NULL;
CREATE INDEX calendar_events_series_idx ON calendar_events (owner_id, series_until) WHERE rrule IS NOT NULL;
CREATE INDEX calendar_events_title_trgm ON calendar_events USING gin (title gin_trgm_ops);

-- Attendees belong to a single event or a series master (exceptions share
-- the master's list). Platform users have user_id; external guests have an
-- email and a hashed RSVP token.
CREATE TABLE calendar_attendees (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id uuid NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
  user_id uuid REFERENCES users(id),
  email text NOT NULL DEFAULT '',
  name text NOT NULL DEFAULT '',
  response text NOT NULL DEFAULT 'needs_action' CHECK (response IN ('needs_action', 'accepted', 'tentative', 'declined')),
  token_hash text UNIQUE,
  responded_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((user_id IS NULL) <> (email = ''))
);
CREATE UNIQUE INDEX calendar_attendees_user_key ON calendar_attendees (event_id, user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX calendar_attendees_email_key ON calendar_attendees (event_id, email) WHERE email <> '';
CREATE INDEX calendar_attendees_user_idx ON calendar_attendees (user_id) WHERE user_id IS NOT NULL;

-- One row per person per occurrence per reminder offset: at-most-once.
CREATE TABLE calendar_reminders_sent (
  event_id uuid NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
  occurrence_start timestamptz NOT NULL,
  user_id uuid NOT NULL REFERENCES users(id),
  offset_minutes integer NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (event_id, occurrence_start, user_id, offset_minutes)
);
CREATE INDEX calendar_reminders_sent_created_idx ON calendar_reminders_sent (created_at);

-- The personal iCal feed: only the sha256 of the secret token is kept.
CREATE TABLE calendar_feeds (
  user_id uuid PRIMARY KEY REFERENCES users(id),
  token_hash text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- External calendars by secret iCal URL (encrypted with CALENDAR_SECRET_KEY).
CREATE TABLE calendar_subscriptions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id),
  name text NOT NULL,
  url_enc bytea NOT NULL,
  url_host text NOT NULL,
  color text NOT NULL DEFAULT 'neutral' CHECK (color IN ('primary', 'accent', 'success', 'warning', 'danger', 'info', 'neutral')),
  enabled boolean NOT NULL DEFAULT true,
  etag text NOT NULL DEFAULT '',
  last_modified text NOT NULL DEFAULT '',
  last_fetched_at timestamptz,
  last_success_at timestamptz,
  last_error text NOT NULL DEFAULT '',
  failures integer NOT NULL DEFAULT 0,
  next_fetch_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calendar_subscriptions_user_idx ON calendar_subscriptions (user_id, created_at);
CREATE INDEX calendar_subscriptions_due_idx ON calendar_subscriptions (next_fetch_at) WHERE enabled;

-- Busy time read from a subscription: times only, never titles.
CREATE TABLE calendar_busy_blocks (
  subscription_id uuid NOT NULL REFERENCES calendar_subscriptions(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id),
  span tstzrange NOT NULL
);
CREATE INDEX calendar_busy_blocks_span_idx ON calendar_busy_blocks USING gist (user_id, span);
CREATE INDEX calendar_busy_blocks_sub_idx ON calendar_busy_blocks (subscription_id);

-- Booking links. Internal links are bookable only by members of org_id.
CREATE TABLE calendar_booking_links (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id uuid NOT NULL REFERENCES users(id),
  org_id uuid REFERENCES orgs(id) ON DELETE CASCADE,
  audience text NOT NULL CHECK (audience IN ('internal', 'external')),
  slug text NOT NULL,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  durations integer[] NOT NULL,
  location_type text NOT NULL CHECK (location_type IN ('video', 'phone', 'in_person', 'custom')),
  location_value text NOT NULL DEFAULT '',
  timezone text NOT NULL,
  weekly_hours jsonb NOT NULL DEFAULT '[]',         -- [{"day":1,"start":540,"end":1020}]
  date_overrides jsonb NOT NULL DEFAULT '[]',       -- [{"date":"2026-11-27","intervals":[]}]
  buffer_before integer NOT NULL DEFAULT 0,
  buffer_after integer NOT NULL DEFAULT 0,
  min_notice integer NOT NULL DEFAULT 240,          -- minutes
  max_days_ahead integer NOT NULL DEFAULT 60,
  max_per_day integer,
  slot_interval integer NOT NULL DEFAULT 30,        -- minutes between slot starts
  questions jsonb NOT NULL DEFAULT '[]',            -- [{"id","label","type","required","options"}]
  confirmation_message text NOT NULL DEFAULT '',
  color text NOT NULL DEFAULT 'primary' CHECK (color IN ('primary', 'accent', 'success', 'warning', 'danger', 'info', 'neutral')),
  active boolean NOT NULL DEFAULT true,
  allow_reschedule boolean NOT NULL DEFAULT true,
  allow_cancel boolean NOT NULL DEFAULT true,
  manage_cutoff integer NOT NULL DEFAULT 60,        -- minutes before the start when booker changes stop
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (audience = 'external' OR org_id IS NOT NULL)
);
CREATE UNIQUE INDEX calendar_booking_links_slug_key ON calendar_booking_links (slug);
CREATE INDEX calendar_booking_links_owner_idx ON calendar_booking_links (owner_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;

-- Bookings. The exclusion constraint makes a double booking of the same
-- host impossible even if two requests race past the availability check.
CREATE TABLE calendar_bookings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  link_id uuid REFERENCES calendar_booking_links(id) ON DELETE SET NULL,
  host_id uuid NOT NULL REFERENCES users(id),
  event_id uuid REFERENCES calendar_events(id) ON DELETE SET NULL,
  title text NOT NULL,
  booker_user_id uuid REFERENCES users(id),
  booker_name text NOT NULL,
  booker_email text NOT NULL,
  booker_timezone text NOT NULL,
  starts_at timestamptz NOT NULL,
  ends_at timestamptz NOT NULL,
  block tstzrange NOT NULL,                         -- starts/ends widened by the link's buffers
  location_type text NOT NULL,
  location_value text NOT NULL DEFAULT '',
  conference_url text,
  conference_id text,
  answers jsonb NOT NULL DEFAULT '[]',
  status text NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed', 'cancelled')),
  cancelled_at timestamptz,
  cancelled_by text CHECK (cancelled_by IN ('host', 'booker', 'system')),
  cancel_reason text NOT NULL DEFAULT '',
  manage_token_hash text NOT NULL UNIQUE,
  reschedule_count integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (ends_at > starts_at),
  CONSTRAINT calendar_bookings_no_overlap EXCLUDE USING gist (host_id WITH =, tstzrange(starts_at, ends_at) WITH &&) WHERE (status = 'confirmed')
);
CREATE INDEX calendar_bookings_host_idx ON calendar_bookings (host_id, starts_at DESC, id DESC);
CREATE INDEX calendar_bookings_link_idx ON calendar_bookings (link_id, starts_at) WHERE status = 'confirmed';
CREATE INDEX calendar_bookings_booker_idx ON calendar_bookings (booker_user_id, starts_at DESC, id DESC) WHERE booker_user_id IS NOT NULL;
CREATE INDEX calendar_bookings_email_idx ON calendar_bookings (lower(booker_email), created_at);
CREATE INDEX calendar_bookings_event_idx ON calendar_bookings (event_id) WHERE event_id IS NOT NULL;

-- Outward side effects queued inside transactions (invites, cancellations,
-- notifications, conference cancellations) and sent after commit by a
-- worker, with retries.
CREATE TABLE calendar_outbox (
  id bigserial PRIMARY KEY,
  kind text NOT NULL CHECK (kind IN ('mail', 'notify', 'conference_cancel')),
  payload jsonb NOT NULL,
  attempts integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calendar_outbox_due_idx ON calendar_outbox (next_attempt_at);
