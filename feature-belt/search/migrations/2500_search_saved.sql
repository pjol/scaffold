-- search: saved searches, a person's named queries (operators included)
-- they run again from the palette, the results page or the phone.
CREATE TABLE search_saved (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id),
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    query      text NOT NULL CHECK (char_length(query) BETWEEN 1 AND 200),
    -- Optional filters saved with it: a kind or feature, and a source.
    kinds      text NOT NULL DEFAULT '' CHECK (char_length(kinds) <= 200),
    source     text NOT NULL DEFAULT '' CHECK (char_length(source) <= 64),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX search_saved_user_idx ON search_saved (user_id, created_at DESC);
