CREATE TABLE IF NOT EXISTS urls (
    id BIGSERIAL PRIMARY KEY,
    original_url TEXT NOT NULL,
    short_code VARCHAR(64) UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ
);

ALTER TABLE urls ADD COLUMN IF NOT EXISTS short_code VARCHAR(64);
ALTER TABLE urls ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
ALTER TABLE urls ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS idx_urls_short_code_unique ON urls(short_code) WHERE short_code IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_urls_expires_at ON urls(expires_at);

CREATE TABLE IF NOT EXISTS click_events (
    id BIGSERIAL PRIMARY KEY,
    short_code VARCHAR(64) NOT NULL,
    ip INET,
    user_agent TEXT,
    referer TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_click_events_code ON click_events(short_code);
CREATE INDEX IF NOT EXISTS idx_click_events_created_at ON click_events(created_at);
