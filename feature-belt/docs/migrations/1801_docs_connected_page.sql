-- docs 1.1.0: a "connected page" block links a Notion page or a Google Doc
-- into a page. It stores only the reference (provider, workspace, platform
-- id, title and link as the person saw them); each viewer opens the content
-- live through their own connection, or is offered to connect.

ALTER TABLE docs_blocks DROP CONSTRAINT IF EXISTS docs_blocks_type_check;
ALTER TABLE docs_blocks ADD CONSTRAINT docs_blocks_type_check CHECK (type IN (
    'paragraph', 'heading_1', 'heading_2', 'heading_3', 'bulleted_list', 'numbered_list',
    'to_do', 'toggle', 'quote', 'callout', 'code', 'divider', 'image', 'file', 'bookmark',
    'page', 'table', 'database', 'connected_page'));
