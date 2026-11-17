-- Leaderboard: people ranked by scores the app provides (feature-belt/leaderboard).

-- How a person appears on boards: under a generated alias (default), their name, or not at all.
CREATE TABLE leaderboard_profiles (
  user_id uuid PRIMARY KEY REFERENCES users(id),
  visibility text NOT NULL DEFAULT 'alias' CHECK (visibility IN ('alias', 'name', 'hidden')),
  alias text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- One score per person per board; rank is computed by the refresh job (null: hidden or not rankable).
CREATE TABLE leaderboard_entries (
  board text NOT NULL,
  user_id uuid NOT NULL REFERENCES users(id),
  score numeric NOT NULL,
  rank int,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (board, user_id)
);
CREATE INDEX leaderboard_entries_rank ON leaderboard_entries (board, rank, user_id) WHERE rank IS NOT NULL;
CREATE INDEX leaderboard_entries_user ON leaderboard_entries (user_id);
