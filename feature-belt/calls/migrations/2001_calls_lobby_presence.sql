-- calls 1.2: people waiting in the lobby check in while their page is open.
-- seen_at is the holder's last check-in; away_at is set when they stop
-- (the page closed, or no check-in for a while) and cleared when they come
-- back. Hosts see, and "Admit all" admits, only people who are still there.

ALTER TABLE calls_lobby
  ADD COLUMN seen_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN away_at timestamptz;
