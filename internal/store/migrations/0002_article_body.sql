-- Richer article pages: a multi-paragraph overview and highlighted key
-- points, in addition to the existing short tldr/why_matters used by cards.
ALTER TABLE articles
    ADD COLUMN IF NOT EXISTS overview   TEXT     NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS key_points TEXT[]   NOT NULL DEFAULT '{}';
