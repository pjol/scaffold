-- docs 1.2.0: the editor's full block set. New types: heading 4, columns
-- (column_list holds 2–5 column blocks), synced blocks (an original holds
-- blocks; a copy stores the original's id and each viewer opens it with
-- their own access), table of contents, breadcrumb, block equation, button,
-- link to page, video, audio, PDF and embed (an allowlisted provider shown
-- in a sandboxed frame). Colours, toggle headings and code captions are
-- props of existing types and need no schema change.

ALTER TABLE docs_blocks DROP CONSTRAINT IF EXISTS docs_blocks_type_check;
ALTER TABLE docs_blocks ADD CONSTRAINT docs_blocks_type_check CHECK (type IN (
    'paragraph', 'heading_1', 'heading_2', 'heading_3', 'heading_4', 'bulleted_list', 'numbered_list',
    'to_do', 'toggle', 'quote', 'callout', 'code', 'divider', 'image', 'file', 'bookmark',
    'page', 'table', 'database', 'connected_page',
    'column_list', 'column', 'synced_block', 'table_of_contents', 'breadcrumb', 'equation', 'button',
    'link_to_page', 'video', 'audio', 'pdf', 'embed'));

-- Synced copies by original, so an original's page can count its copies.
CREATE INDEX docs_blocks_synced_source_idx ON docs_blocks ((props->>'source_id')) WHERE type = 'synced_block' AND props ? 'source_id';
