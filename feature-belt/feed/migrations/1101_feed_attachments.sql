-- feed: document attachments (PDF, Office, text, zip) beside photos and video.
-- Documents are stored under the feed_file upload purpose; name is the file
-- name shown on the post.

ALTER TABLE feed_media ADD COLUMN name text;
ALTER TABLE feed_media DROP CONSTRAINT feed_media_kind_check;
ALTER TABLE feed_media ADD CONSTRAINT feed_media_kind_check CHECK (kind IN ('image', 'video', 'file'));
