-- calls: meetings (stable links with a Meet-style code), invitees and
-- co-hosts, sessions (each time a meeting's room is live), attendance, the
-- lobby, instant-call rings and per-session removals.

CREATE TABLE calls_meetings (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  code          text NOT NULL UNIQUE CHECK (code ~ '^[a-z]{3}-[a-z]{4}-[a-z]{3}$'),
  kind          text NOT NULL DEFAULT 'meeting' CHECK (kind IN ('meeting', 'call')),
  title         text NOT NULL DEFAULT '' CHECK (char_length(title) <= 200),
  host_id       uuid NOT NULL REFERENCES users(id),
  org_id        uuid REFERENCES orgs(id) ON DELETE CASCADE,
  access        text NOT NULL CHECK (access IN ('org', 'invited', 'open')),
  lobby_enabled boolean NOT NULL DEFAULT true,
  locked        boolean NOT NULL DEFAULT false,
  starts_at     timestamptz,
  ends_at       timestamptz,
  source_kind   text,
  source_id     text,
  canceled_at   timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CHECK (access <> 'org' OR org_id IS NOT NULL),
  CHECK (ends_at IS NULL OR (starts_at IS NOT NULL AND ends_at > starts_at)),
  CHECK ((source_kind IS NULL) = (source_id IS NULL))
);
CREATE INDEX calls_meetings_host_idx ON calls_meetings (host_id, created_at DESC, id DESC);
CREATE INDEX calls_meetings_org_idx ON calls_meetings (org_id) WHERE org_id IS NOT NULL;
CREATE INDEX calls_meetings_starts_idx ON calls_meetings (starts_at, id) WHERE starts_at IS NOT NULL AND canceled_at IS NULL;
-- One active meeting per source record (a calendar event, a channel).
CREATE UNIQUE INDEX calls_meetings_source_idx ON calls_meetings (source_kind, source_id)
  WHERE source_kind IS NOT NULL AND canceled_at IS NULL;

-- People invited by account. role cohost shares the host's controls.
CREATE TABLE calls_invitees (
  meeting_id uuid NOT NULL REFERENCES calls_meetings(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id),
  role       text NOT NULL DEFAULT 'invitee' CHECK (role IN ('invitee', 'cohost')),
  invited_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (meeting_id, user_id)
);
CREATE INDEX calls_invitees_user_idx ON calls_invitees (user_id, meeting_id);

-- Each time a meeting's room is live. room_sid is the SFU's id for that
-- room instance (set when the SFU reports it).
CREATE TABLE calls_sessions (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  meeting_id uuid NOT NULL REFERENCES calls_meetings(id) ON DELETE CASCADE,
  room_sid   text UNIQUE,
  started_at timestamptz NOT NULL DEFAULT now(),
  ended_at   timestamptz,
  end_reason text CHECK (end_reason IN ('empty', 'host_ended', 'canceled', 'reconciled')),
  ended_by   uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX calls_sessions_live_idx ON calls_sessions (meeting_id) WHERE ended_at IS NULL;
CREATE INDEX calls_sessions_meeting_idx ON calls_sessions (meeting_id, started_at DESC, id DESC);

-- Attendance, one row per join. Guests have no user_id; name keeps what
-- they typed (people show their current name).
CREATE TABLE calls_participants (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id      uuid NOT NULL REFERENCES calls_sessions(id) ON DELETE CASCADE,
  identity        text NOT NULL,
  user_id         uuid REFERENCES users(id),
  name            text NOT NULL DEFAULT '',
  participant_sid text,
  joined_at       timestamptz NOT NULL,
  left_at         timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calls_participants_session_idx ON calls_participants (session_id, joined_at);
CREATE INDEX calls_participants_user_idx ON calls_participants (user_id, session_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX calls_participants_sid_idx ON calls_participants (participant_sid) WHERE participant_sid IS NOT NULL;

-- People asking to join. Guests hold a secret key; only its sha256 is kept.
CREATE TABLE calls_lobby (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  meeting_id uuid NOT NULL REFERENCES calls_meetings(id) ON DELETE CASCADE,
  user_id    uuid REFERENCES users(id),
  guest_name text CHECK (char_length(guest_name) <= 60),
  key_hash   bytea,
  status     text NOT NULL DEFAULT 'waiting'
             CHECK (status IN ('waiting', 'admitted', 'denied', 'canceled', 'expired', 'removed')),
  decided_by uuid REFERENCES users(id),
  decided_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((user_id IS NULL) = (key_hash IS NOT NULL)),
  CHECK (user_id IS NOT NULL OR guest_name IS NOT NULL)
);
CREATE INDEX calls_lobby_waiting_idx ON calls_lobby (meeting_id, created_at) WHERE status = 'waiting';
CREATE INDEX calls_lobby_user_idx ON calls_lobby (user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX calls_lobby_one_waiting_idx ON calls_lobby (meeting_id, user_id) WHERE status = 'waiting' AND user_id IS NOT NULL;

-- Instant calls ringing people's devices.
CREATE TABLE calls_rings (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  meeting_id  uuid NOT NULL REFERENCES calls_meetings(id) ON DELETE CASCADE,
  caller_id   uuid NOT NULL REFERENCES users(id),
  user_id     uuid NOT NULL REFERENCES users(id),
  status      text NOT NULL DEFAULT 'ringing' CHECK (status IN ('ringing', 'accepted', 'declined', 'missed')),
  expires_at  timestamptz NOT NULL,
  answered_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calls_rings_user_idx ON calls_rings (user_id, created_at DESC, id DESC);
CREATE INDEX calls_rings_meeting_idx ON calls_rings (meeting_id);
CREATE INDEX calls_rings_expiry_idx ON calls_rings (expires_at) WHERE status = 'ringing';
CREATE UNIQUE INDEX calls_rings_one_idx ON calls_rings (meeting_id, user_id) WHERE status = 'ringing';

-- People a host removed: they can't rejoin that session.
CREATE TABLE calls_removals (
  session_id uuid NOT NULL REFERENCES calls_sessions(id) ON DELETE CASCADE,
  identity   text NOT NULL,
  removed_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (session_id, identity)
);
