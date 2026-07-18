-- Phase 1 schema: the database is the source of truth for the dynamic product.
-- Articles are never deleted, so archives persist and URLs stay stable.

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS articles (
    id            BIGSERIAL PRIMARY KEY,
    url           TEXT        NOT NULL UNIQUE,
    slug          TEXT        NOT NULL UNIQUE,
    title         TEXT        NOT NULL,
    source_name   TEXT        NOT NULL DEFAULT '',
    author        TEXT        NOT NULL DEFAULT '',
    content_hash  TEXT        NOT NULL DEFAULT '',
    tldr          TEXT        NOT NULL DEFAULT '',
    why_matters   TEXT        NOT NULL DEFAULT '',
    topic         TEXT        NOT NULL DEFAULT '',
    embedding     vector,
    published_at  TIMESTAMPTZ NOT NULL,
    fetched_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_articles_topic_published
    ON articles (topic, published_at DESC);

CREATE TABLE IF NOT EXISTS editions (
    id            BIGSERIAL PRIMARY KEY,
    slug          TEXT        NOT NULL UNIQUE,
    title         TEXT        NOT NULL,
    topic         TEXT        NOT NULL DEFAULT '',
    language      TEXT        NOT NULL DEFAULT 'en',
    published_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_editions_topic_published
    ON editions (topic, published_at DESC);

CREATE TABLE IF NOT EXISTS edition_articles (
    edition_id  BIGINT  NOT NULL REFERENCES editions (id) ON DELETE CASCADE,
    article_id  BIGINT  NOT NULL REFERENCES articles (id) ON DELETE CASCADE,
    rank        INT     NOT NULL DEFAULT 0,
    score       DOUBLE PRECISION NOT NULL DEFAULT 0,
    PRIMARY KEY (edition_id, article_id)
);

CREATE INDEX IF NOT EXISTS idx_edition_articles_article
    ON edition_articles (article_id);
