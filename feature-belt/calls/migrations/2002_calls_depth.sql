-- calls 1.4: host controls, recordings, saved transcripts and chat, polls,
-- Q&A, breakout rooms and workspace call settings.

-- Room rules hosts set (they apply to people who aren't hosts) and who may
-- open the meeting's records (recordings, transcripts, saved chat).
ALTER TABLE calls_meetings
  ADD COLUMN allow_screen_share boolean NOT NULL DEFAULT true,
  ADD COLUMN allow_chat         boolean NOT NULL DEFAULT true,
  ADD COLUMN allow_reactions    boolean NOT NULL DEFAULT true,
  ADD COLUMN allow_unmute       boolean NOT NULL DEFAULT true,
  ADD COLUMN mute_on_entry      boolean NOT NULL DEFAULT false,
  ADD COLUMN save_chat          boolean NOT NULL DEFAULT false,
  ADD COLUMN moderate_questions boolean NOT NULL DEFAULT false,
  ADD COLUMN records_access     text NOT NULL DEFAULT 'participants'
    CHECK (records_access IN ('participants', 'invited', 'org')),
  ADD CONSTRAINT calls_meetings_records_org CHECK (records_access <> 'org' OR org_id IS NOT NULL);

-- Live room state: who is spotlighted for everyone, whether the transcript
-- is being saved, and people a host let speak while unmuting is off.
ALTER TABLE calls_sessions
  ADD COLUMN spotlight     text,
  ADD COLUMN transcribing  boolean NOT NULL DEFAULT false,
  ADD COLUMN speakers      text[] NOT NULL DEFAULT '{}';

-- Time spent in a breakout room counts toward the same session.
ALTER TABLE calls_participants ADD COLUMN breakout_room_id uuid;

-- Recordings (an SFU egress writing one MP4 to object storage).
CREATE TABLE calls_recordings (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  meeting_id  uuid NOT NULL REFERENCES calls_meetings(id) ON DELETE CASCADE,
  session_id  uuid REFERENCES calls_sessions(id) ON DELETE SET NULL,
  egress_id   text UNIQUE,
  status      text NOT NULL DEFAULT 'starting'
              CHECK (status IN ('starting', 'recording', 'processing', 'ready', 'failed')),
  started_by  uuid REFERENCES users(id),
  stopped_by  uuid REFERENCES users(id),
  started_at  timestamptz NOT NULL DEFAULT now(),
  ended_at    timestamptz,
  storage_key text NOT NULL,
  size_bytes  bigint,
  duration_ms bigint,
  error       text,
  expires_at  timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calls_recordings_meeting_idx ON calls_recordings (meeting_id, started_at DESC, id DESC);
CREATE INDEX calls_recordings_session_idx ON calls_recordings (session_id) WHERE session_id IS NOT NULL;
CREATE UNIQUE INDEX calls_recordings_active_idx ON calls_recordings (meeting_id) WHERE status IN ('starting', 'recording');
CREATE INDEX calls_recordings_expiry_idx ON calls_recordings (expires_at) WHERE expires_at IS NOT NULL;

-- Transcript lines: each speaker's own captions, saved while a host has
-- the transcript on.
CREATE TABLE calls_transcript_lines (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES calls_sessions(id) ON DELETE CASCADE,
  identity   text NOT NULL,
  user_id    uuid REFERENCES users(id),
  name       text NOT NULL DEFAULT '',
  body       text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
  spoken_at  timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calls_transcript_session_idx ON calls_transcript_lines (session_id, spoken_at, id);
CREATE INDEX calls_transcript_user_idx ON calls_transcript_lines (user_id) WHERE user_id IS NOT NULL;

-- Chat messages kept while the meeting saves chat.
CREATE TABLE calls_chat_messages (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES calls_sessions(id) ON DELETE CASCADE,
  identity   text NOT NULL,
  user_id    uuid REFERENCES users(id),
  name       text NOT NULL DEFAULT '',
  body       text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
  sent_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calls_chat_session_idx ON calls_chat_messages (session_id, sent_at, id);
CREATE INDEX calls_chat_user_idx ON calls_chat_messages (user_id) WHERE user_id IS NOT NULL;

-- Polls, prepared ahead or during a call, kept with the meeting.
CREATE TABLE calls_polls (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  meeting_id     uuid NOT NULL REFERENCES calls_meetings(id) ON DELETE CASCADE,
  session_id     uuid REFERENCES calls_sessions(id) ON DELETE SET NULL,
  question       text NOT NULL CHECK (char_length(question) BETWEEN 1 AND 300),
  options        text[] NOT NULL CHECK (cardinality(options) BETWEEN 2 AND 10),
  multiple       boolean NOT NULL DEFAULT false,
  anonymous      boolean NOT NULL DEFAULT true,
  status         text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'open', 'closed')),
  results_shared boolean NOT NULL DEFAULT false,
  created_by     uuid REFERENCES users(id),
  opened_at      timestamptz,
  closed_at      timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calls_polls_meeting_idx ON calls_polls (meeting_id, created_at, id);

CREATE TABLE calls_poll_votes (
  poll_id    uuid NOT NULL REFERENCES calls_polls(id) ON DELETE CASCADE,
  identity   text NOT NULL,
  user_id    uuid REFERENCES users(id),
  name       text NOT NULL DEFAULT '',
  choices    smallint[] NOT NULL CHECK (cardinality(choices) >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (poll_id, identity)
);
CREATE INDEX calls_poll_votes_user_idx ON calls_poll_votes (user_id) WHERE user_id IS NOT NULL;

-- Questions people ask in a call; hosts answer, hide or approve them.
CREATE TABLE calls_questions (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  meeting_id uuid NOT NULL REFERENCES calls_meetings(id) ON DELETE CASCADE,
  session_id uuid REFERENCES calls_sessions(id) ON DELETE SET NULL,
  identity   text NOT NULL,
  user_id    uuid REFERENCES users(id),
  name       text NOT NULL DEFAULT '',
  anonymous  boolean NOT NULL DEFAULT false,
  body       text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 500),
  status     text NOT NULL DEFAULT 'open' CHECK (status IN ('pending', 'open', 'answered', 'hidden')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calls_questions_meeting_idx ON calls_questions (meeting_id, created_at, id);
CREATE INDEX calls_questions_user_idx ON calls_questions (user_id) WHERE user_id IS NOT NULL;

CREATE TABLE calls_question_votes (
  question_id uuid NOT NULL REFERENCES calls_questions(id) ON DELETE CASCADE,
  identity    text NOT NULL,
  user_id     uuid REFERENCES users(id),
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (question_id, identity)
);
CREATE INDEX calls_question_votes_user_idx ON calls_question_votes (user_id) WHERE user_id IS NOT NULL;

-- Breakout rooms: one set at a time per session, each room its own SFU
-- room named by the room's id.
CREATE TABLE calls_breakouts (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  meeting_id   uuid NOT NULL REFERENCES calls_meetings(id) ON DELETE CASCADE,
  session_id   uuid NOT NULL REFERENCES calls_sessions(id) ON DELETE CASCADE,
  status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closing', 'closed')),
  allow_choose boolean NOT NULL DEFAULT false,
  ends_at      timestamptz,
  closing_at   timestamptz,
  created_by   uuid REFERENCES users(id),
  created_at   timestamptz NOT NULL DEFAULT now(),
  closed_at    timestamptz
);
CREATE UNIQUE INDEX calls_breakouts_live_idx ON calls_breakouts (session_id) WHERE status <> 'closed';
CREATE INDEX calls_breakouts_due_idx ON calls_breakouts (status, ends_at, closing_at) WHERE status <> 'closed';

CREATE TABLE calls_breakout_rooms (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  breakout_id uuid NOT NULL REFERENCES calls_breakouts(id) ON DELETE CASCADE,
  name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
  position    int NOT NULL,
  UNIQUE (breakout_id, position)
);

CREATE TABLE calls_breakout_assignments (
  breakout_id uuid NOT NULL REFERENCES calls_breakouts(id) ON DELETE CASCADE,
  identity    text NOT NULL,
  room_id     uuid NOT NULL REFERENCES calls_breakout_rooms(id) ON DELETE CASCADE,
  user_id     uuid REFERENCES users(id),
  name        text NOT NULL DEFAULT '',
  PRIMARY KEY (breakout_id, identity)
);
CREATE INDEX calls_breakout_assignments_user_idx ON calls_breakout_assignments (user_id) WHERE user_id IS NOT NULL;

-- What a workspace allows in its calls. Captions use each speaker's
-- browser speech recognition (audio goes to the browser's vendor), so
-- they are off until an admin turns them on.
CREATE TABLE calls_org_settings (
  org_id      uuid PRIMARY KEY REFERENCES orgs(id) ON DELETE CASCADE,
  captions    boolean NOT NULL DEFAULT false,
  transcripts boolean NOT NULL DEFAULT false,
  recording   boolean NOT NULL DEFAULT true,
  updated_by  uuid REFERENCES users(id),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
