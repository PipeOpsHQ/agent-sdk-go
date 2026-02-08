CREATE TABLE IF NOT EXISTS run_attempts (
  run_id TEXT NOT NULL,
  attempt INTEGER NOT NULL,
  worker_id TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  error TEXT NOT NULL DEFAULT '',
  metadata TEXT NOT NULL DEFAULT '{}',
  PRIMARY KEY (run_id, attempt)
);

CREATE INDEX IF NOT EXISTS idx_run_attempts_run_id ON run_attempts(run_id);
CREATE INDEX IF NOT EXISTS idx_run_attempts_worker_id ON run_attempts(worker_id);
CREATE INDEX IF NOT EXISTS idx_run_attempts_started_at ON run_attempts(started_at DESC);

CREATE TABLE IF NOT EXISTS worker_heartbeats (
  worker_id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  capacity INTEGER NOT NULL DEFAULT 1,
  metadata TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_worker_heartbeats_last_seen ON worker_heartbeats(last_seen_at DESC);

CREATE TABLE IF NOT EXISTS queue_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL,
  event TEXT NOT NULL,
  at TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_queue_events_run_id ON queue_events(run_id);
CREATE INDEX IF NOT EXISTS idx_queue_events_at ON queue_events(at DESC);
