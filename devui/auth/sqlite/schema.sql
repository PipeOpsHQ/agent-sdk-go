CREATE TABLE IF NOT EXISTS api_keys (
  id TEXT PRIMARY KEY,
  key_hash TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL,
  created_at TEXT NOT NULL,
  rotated_at TEXT,
  disabled_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_api_keys_role ON api_keys(role);
CREATE INDEX IF NOT EXISTS idx_api_keys_disabled_at ON api_keys(disabled_at);
