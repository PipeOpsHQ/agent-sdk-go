CREATE TABLE IF NOT EXISTS trace_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT,
  run_id TEXT,
  session_id TEXT,
  span_id TEXT,
  parent_span_id TEXT,
  kind TEXT NOT NULL,
  status TEXT,
  name TEXT,
  provider TEXT,
  tool_name TEXT,
  message TEXT,
  error TEXT,
  duration_ms INTEGER DEFAULT 0,
  attributes TEXT NOT NULL DEFAULT '{}',
  timestamp TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_trace_events_run_id ON trace_events(run_id);
CREATE INDEX IF NOT EXISTS idx_trace_events_session_id ON trace_events(session_id);
CREATE INDEX IF NOT EXISTS idx_trace_events_timestamp ON trace_events(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_trace_events_kind_status ON trace_events(kind, status);
