// ===== API Client =====
const api = {
  get: async (path) => {
    const res = await fetch(path, { headers: authHeaders() });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
  },
  post: async (path, body) => {
    const res = await fetch(path, {
      method: 'POST',
      headers: { ...authHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
  },
  request: async (path, opts = {}) => {
    const res = await fetch(path, {
      ...opts,
      headers: { ...authHeaders(), 'Content-Type': 'application/json', ...(opts.headers || {}) },
    });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    const text = await res.text();
    return text ? JSON.parse(text) : {};
  },
};

function authHeaders() {
  const key = localStorage.getItem('devui_api_key');
  if (!key) return {};
  return { 'X-API-Key': key };
}

// ===== State =====
let currentRun = null;
let runs = [];
let recentActivity = [];
let currentRunEvents = [];
let currentRunAttempts = [];
let selectedGraphWorkflow = '';
let playgroundSessionId = ''; // tracks multi-turn conversation session

// ===== Utilities =====
function formatDate(dateStr) {
  if (!dateStr) return '-';
  const d = new Date(dateStr);
  return d.toLocaleString('en-US', {
    month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit'
  });
}

function formatDuration(ms) {
  if (!ms) return '-';
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function truncate(str, len = 40) {
  if (!str) return '';
  return str.length > len ? str.slice(0, len) + '...' : str;
}

function escapeHtml(str) {
  if (!str) return '';
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function runIdOf(run) {
  return run?.runId || run?.runID || '';
}

function runStatusOf(run) {
  return String(run?.status || 'pending').toLowerCase();
}

function runTimestamp(run) {
  return run?.updatedAt || run?.createdAt || '';
}

function toDateSafe(value) {
  if (!value) return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return null;
  return d;
}

function inferEventKind(event) {
  const raw = String(event?.kind || event?.type || event?.eventType || '').toLowerCase();
  if (raw.includes('tool')) return 'tool';
  if (raw.includes('retry')) return 'retry';
  if (raw.includes('router') || raw.includes('route') || raw.includes('graph')) return 'router';
  if (raw.includes('checkpoint')) return 'checkpoint';
  if (raw.includes('error') || raw.includes('fail')) return 'error';
  if (raw.includes('generate') || raw.includes('provider') || raw.includes('llm')) return 'generate';
  return 'event';
}

// ===== Theme =====
function initTheme() {
  const saved = localStorage.getItem('theme');
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  const theme = saved || (prefersDark ? 'dark' : 'light');
  document.documentElement.setAttribute('data-theme', theme);

  document.getElementById('themeToggle')?.addEventListener('click', () => {
    const current = document.documentElement.getAttribute('data-theme');
    const next = current === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', next);
    localStorage.setItem('theme', next);
  });
}

// ===== Navigation =====
function switchTab(tab) {
  if (!tab) return;
  if (tab === 'dashboard') tab = 'live';
  document.querySelectorAll('.nav-item[data-tab]').forEach(n => {
    n.classList.toggle('active', n.getAttribute('data-tab') === tab);
  });
  document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
  document.getElementById(`tab-${tab}`)?.classList.add('active');
  if (tab === 'scheduler') loadCronJobs();
  if (tab === 'actions') loadActions();
}

function initNavigation() {
  document.querySelectorAll('[data-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      const tab = btn.getAttribute('data-tab');
      switchTab(tab);
    });
  });

  // Run detail tabs
  document.querySelectorAll('[data-run-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      const tab = btn.getAttribute('data-run-tab');

      document.querySelectorAll('.run-tab').forEach(t => t.classList.remove('active'));
      btn.classList.add('active');

      document.querySelectorAll('.run-panel').forEach(p => p.classList.remove('active'));
      document.getElementById(`run-${tab}`)?.classList.add('active');
    });
  });

  // Tools tabs
  document.querySelectorAll('[data-tools-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      const tab = btn.getAttribute('data-tools-tab');

      document.querySelectorAll('.tools-tab').forEach(t => t.classList.remove('active'));
      btn.classList.add('active');

      document.querySelectorAll('.tools-panel').forEach(p => p.classList.remove('active'));
      document.getElementById(`tools-${tab}`)?.classList.add('active');
    });
  });
}

// ===== Dashboard =====
async function loadDashboard() {
  try {
    const [metrics, runtime, registry, recentRuns] = await Promise.all([
      api.get('/api/v1/metrics/summary').catch(() => ({})),
      api.get('/api/v1/runtime/details').catch(() => ({ available: false, status: 'unavailable' })),
      api.get('/api/v1/tools/registry').catch(() => ({ toolCount: 0 })),
      api.get('/api/v1/runs?limit=1').catch(() => []),
    ]);

    // Update metrics
    const completed = metrics.runsCompleted || metrics.completed || metrics.total_completed || 0;
    const failed = metrics.runsFailed || metrics.failed || metrics.total_failed || 0;
    const started = metrics.runsStarted || 0;
    const running = Math.max(0, started - completed - failed) || metrics.running || metrics.in_progress || 0;
    const tools = registry.toolCount || metrics.tools || metrics.active_tools || 0;

    document.getElementById('metric-completed').textContent = completed;
    document.getElementById('metric-running').textContent = running;
    document.getElementById('metric-failed').textContent = failed;
    document.getElementById('metric-tools').textContent = tools;
    const providerEl = document.getElementById('status-provider');
    if (providerEl) {
      const runs = Array.isArray(recentRuns) ? recentRuns : [];
      const provider = runs[0]?.provider || metrics.provider || metrics.primaryProvider || 'n/a';
      providerEl.textContent = provider;
    }

    // Update status indicators
    const queueDot = document.getElementById('status-queue-dot');
    const workersDot = document.getElementById('status-workers-dot');

    if (runtime.available) {
      queueDot?.classList.add('online');
      queueDot?.classList.remove('offline');
      const pending = runtime.queue?.pending || 0;
      document.getElementById('status-queue').textContent = `${pending} pending`;
    } else {
      queueDot?.classList.add('offline');
      queueDot?.classList.remove('online');
      document.getElementById('status-queue').textContent = 'Unavailable';
    }

    const workerCount = runtime.workerCount || 0;
    if (workerCount > 0) {
      workersDot?.classList.add('online');
      workersDot?.classList.remove('offline');
      document.getElementById('status-workers').textContent = `${workerCount} active`;
    } else {
      workersDot?.classList.add('offline');
      workersDot?.classList.remove('online');
      document.getElementById('status-workers').textContent = 'None';
    }

  } catch (e) {
    console.error('Dashboard load error:', e);
  }
}

async function loadRecentActivity() {
  const container = document.getElementById('activityList');
  const liveStrip = document.getElementById('liveRunsStrip');
  if (!container && !liveStrip) return;

  try {
    const recentRuns = await api.get('/api/v1/runs?limit=12');
    const rows = Array.isArray(recentRuns) ? recentRuns : [];
    if (!rows.length) {
      if (container) container.innerHTML = '<div class="empty-state"><p>No recent activity</p></div>';
      if (liveStrip) liveStrip.innerHTML = '<div class="empty-state"><p>No active entities</p></div>';
      return;
    }

    if (container) {
      container.innerHTML = rows.map(run => `
      <div class="activity-item activity-${runStatusOf(run)}">
        <div class="activity-dot ${runStatusOf(run)}"></div>
        <div class="activity-content">
          <div class="activity-title">${escapeHtml(truncate(runIdOf(run), 24))}</div>
          <div class="activity-meta">${escapeHtml(run.provider || 'unknown')} • ${formatDate(run.updatedAt)}</div>
        </div>
      </div>
    `).join('');
    }

    if (liveStrip) {
      liveStrip.innerHTML = rows.slice(0, 8).map(run => {
        const runID = runIdOf(run);
        const status = runStatusOf(run);
        return `
          <button class="run-entity ${status}" data-live-run-id="${escapeHtml(runID)}" title="Open run ${escapeHtml(runID)}">
            <span class="run-entity-pulse"></span>
            <span class="run-entity-id">${escapeHtml(truncate(runID, 18))}</span>
            <span class="run-entity-meta">${escapeHtml(run.provider || 'unknown')}</span>
          </button>
        `;
      }).join('');
      liveStrip.querySelectorAll('[data-live-run-id]').forEach(btn => {
        btn.addEventListener('click', () => {
          const runID = btn.getAttribute('data-live-run-id');
          if (!runID) return;
          switchTab('runs');
          selectRun(runID);
        });
      });
    }
  } catch (e) {
    if (container) container.innerHTML = '<div class="empty-state"><p>Failed to load activity</p></div>';
    if (liveStrip) liveStrip.innerHTML = '<div class="empty-state"><p>Entity stream unavailable</p></div>';
  }
}

// ===== Runs =====
async function loadRuns() {
  const container = document.getElementById('runsList');
  if (!container) return;

  try {
    runs = await api.get('/api/v1/runs?limit=100');

    if (!runs || runs.length === 0) {
      container.innerHTML = '<div class="empty-state"><p>No runs found</p></div>';
      return;
    }

    container.innerHTML = runs.map(run => `
      <div class="run-item ${currentRun === runIdOf(run) ? 'selected' : ''}" data-run-id="${runIdOf(run)}">
        <div class="run-item-status ${runStatusOf(run)}"></div>
        <div class="run-item-content">
          <div class="run-item-id">${escapeHtml(truncate(runIdOf(run), 20))}</div>
          <div class="run-item-meta">${runStatusOf(run)} • ${formatDate(runTimestamp(run))}</div>
        </div>
      </div>
    `).join('');

    // Add click handlers
    container.querySelectorAll('.run-item').forEach(item => {
      item.addEventListener('click', () => {
        const runId = item.getAttribute('data-run-id');
        selectRun(runId);
      });
    });
    syncGraphRunOptions(runs);
  } catch (e) {
    container.innerHTML = '<div class="empty-state"><p>Failed to load runs</p></div>';
  }
}

async function selectRun(runId) {
  currentRun = runId;

  // Update selection UI
  document.querySelectorAll('.run-item').forEach(item => {
    item.classList.toggle('selected', item.getAttribute('data-run-id') === runId);
  });

  try {
    const [run, events, attempts] = await Promise.all([
      api.get(`/api/v1/runs/${runId}`),
      api.get(`/api/v1/runs/${runId}/events?limit=500`).catch(() => []),
      api.get(`/api/v1/runtime/runs/${runId}/attempts?limit=100`).catch(() => []),
    ]);
    currentRunEvents = Array.isArray(events) ? events : [];
    currentRunAttempts = Array.isArray(attempts) ? attempts : [];

    // Update header
    const header = document.getElementById('runDetailHeader');
    if (header) {
      header.innerHTML = `
        <div style="display: flex; align-items: center; gap: 12px;">
          <h2>${escapeHtml(truncate(runId, 24))}</h2>
          <span class="badge status-${run.status || 'pending'}">${run.status || 'pending'}</span>
        </div>
        <p>${run.provider || 'unknown'} • Started ${formatDate(run.createdAt)}</p>
      `;
    }

    // Update overview
    document.getElementById('runDetail').textContent = JSON.stringify(run, null, 2);

    // Update timeline
    renderTimeline(currentRunEvents);
    renderCognitiveMetrics(currentRunEvents, currentRunAttempts);
    renderAttemptLane(currentRunAttempts);
    renderExecutionState(run, currentRunAttempts, currentRunEvents);
    await loadInterventions(runId);

    // Update messages
    renderMessages(currentRunEvents, run);

    // Update tool calls
    renderToolCalls(currentRunEvents, run);

    // Update trace tree
    renderTraceTree(run, currentRunEvents);

  } catch (e) {
    console.error('Failed to load run details:', e);
  }
}

function renderTimeline(events) {
  const container = document.getElementById('runTimeline');
  if (!container) return;

  if (!events || events.length === 0) {
    container.innerHTML = '<div class="empty-state"><p>No cognitive events</p></div>';
    return;
  }

  container.innerHTML = events.slice(0, 60).map(event => {
    const type = event.type || event.eventType || event.kind || 'event';
    const kind = inferEventKind(event);
    const statusRaw = String(event.status || type || '').toLowerCase();
    const statusClass = statusRaw.includes('error') || statusRaw.includes('fail')
      ? 'error'
      : (statusRaw.includes('complete') || statusRaw.includes('success') ? 'success' : 'warning');
    const duration = Number(event.durationMs || event.duration_ms || event?.attributes?.durationMs || 0);
    const confidence = event?.attributes?.confidence;
    const attrs = event.attributes || {};
    const payload = event.data || event.payload || attrs || {};
    const signalLabel = kind === 'generate'
      ? 'Generate'
      : (kind === 'tool'
          ? 'Tool Call'
          : (kind === 'router'
              ? 'Router'
              : (kind === 'retry'
                  ? 'Retry'
                  : (kind === 'checkpoint' ? 'Checkpoint' : 'Event'))));

    return `
      <div class="timeline-item ${statusClass} kind-${kind}">
        <div class="timeline-head">
          <span class="timeline-kind">${signalLabel}</span>
          <span class="timeline-time">${formatDate(event.timestamp)}</span>
        </div>
        <div class="timeline-title">${escapeHtml(type)}</div>
        <div class="timeline-meta">
          ${duration > 0 ? `<span>duration ${duration}ms</span>` : '<span>duration n/a</span>'}
          ${confidence !== undefined ? `<span>confidence ${escapeHtml(String(confidence))}</span>` : '<span>confidence n/a</span>'}
          <span>status ${escapeHtml(statusRaw || 'observed')}</span>
        </div>
        <div class="timeline-content">${escapeHtml(truncate(JSON.stringify(payload), 160))}</div>
      </div>
    `;
  }).join('');
}

function renderCognitiveMetrics(events, attempts) {
  const container = document.getElementById('runCognitiveMetrics');
  if (!container) return;
  const rows = Array.isArray(events) ? events : [];
  const counts = {
    generate: 0,
    tool: 0,
    router: 0,
    retry: 0,
    checkpoint: 0,
    error: 0,
  };
  let durationTotal = 0;
  let durationCount = 0;
  rows.forEach((event) => {
    const kind = inferEventKind(event);
    if (counts[kind] !== undefined) counts[kind] += 1;
    const duration = Number(event.durationMs || event.duration_ms || event?.attributes?.durationMs || 0);
    if (duration > 0) {
      durationTotal += duration;
      durationCount += 1;
    }
  });
  const attemptsCount = Array.isArray(attempts) ? attempts.length : 0;
  const avgDuration = durationCount > 0 ? Math.round(durationTotal / durationCount) : 0;
  container.innerHTML = `
    <div class="cognitive-chip">Generate: <strong>${counts.generate}</strong></div>
    <div class="cognitive-chip">Tool calls: <strong>${counts.tool}</strong></div>
    <div class="cognitive-chip">Router decisions: <strong>${counts.router}</strong></div>
    <div class="cognitive-chip">Retries: <strong>${counts.retry}</strong></div>
    <div class="cognitive-chip">Checkpoints: <strong>${counts.checkpoint}</strong></div>
    <div class="cognitive-chip ${counts.error > 0 ? 'danger' : ''}">Errors: <strong>${counts.error}</strong></div>
    <div class="cognitive-chip">Avg step: <strong>${avgDuration || 0}ms</strong></div>
    <div class="cognitive-chip">Attempts: <strong>${attemptsCount}</strong></div>
  `;
}

function renderAttemptLane(attempts) {
  const container = document.getElementById('runAttemptLane');
  if (!container) return;
  const rows = Array.isArray(attempts) ? attempts : [];
  if (!rows.length) {
    container.innerHTML = '<div class="empty-state"><p>No distributed attempts recorded for this run.</p></div>';
    return;
  }
  const sorted = rows.slice().sort((a, b) => (a.attempt || 0) - (b.attempt || 0));
  const parts = [];
  sorted.forEach((item, idx) => {
    const status = String(item.status || 'unknown').toLowerCase();
    const startedAt = toDateSafe(item.startedAt);
    const prevEnded = idx > 0 ? toDateSafe(sorted[idx - 1].endedAt) : null;
    let backoffMs = null;
    if (startedAt && prevEnded) {
      const gap = startedAt.getTime() - prevEnded.getTime();
      backoffMs = gap > 0 ? gap : 0;
    }
    parts.push(`
      <div class="attempt-node ${status}">
        <div class="attempt-num">A${escapeHtml(String(item.attempt || idx + 1))}</div>
        <div class="attempt-status">${escapeHtml(status)}</div>
        <div class="attempt-worker">${escapeHtml(item.workerId || 'worker?')}</div>
        ${backoffMs !== null ? `<div class="attempt-backoff">backoff ${backoffMs}ms</div>` : '<div class="attempt-backoff">start</div>'}
      </div>
      ${idx < sorted.length - 1 ? '<div class="attempt-link"></div>' : ''}
    `);
  });
  container.innerHTML = `<div class="attempt-track">${parts.join('')}</div>`;
}

function renderExecutionState(run, attempts, events) {
  const container = document.getElementById('runExecutionState');
  if (!container) return;
  const checkpoints = events.filter((e) => inferEventKind(e) === 'checkpoint').length;
  const retries = events.filter((e) => inferEventKind(e) === 'retry').length;
  const latestAttempt = Array.isArray(attempts) && attempts.length ? attempts[0] : null;
  const metadata = run?.metadata || {};

  // Count tool calls from run.messages if available, else from events
  const runMessages = Array.isArray(run?.messages) ? run.messages : [];
  let toolCalls = 0;
  runMessages.forEach(m => { if (m.toolCalls?.length) toolCalls += m.toolCalls.length; });
  if (toolCalls === 0) toolCalls = events.filter((e) => inferEventKind(e) === 'tool').length;

  // Count tokens from usage
  const usage = run?.usage || {};
  const tokens = usage.totalTokens || usage.total_tokens || 0;

  container.innerHTML = `
    <div class="exec-row"><span>Status</span><strong>${escapeHtml(runStatusOf(run))}</strong></div>
    <div class="exec-row"><span>Provider</span><strong>${escapeHtml(run?.provider || 'unknown')}</strong></div>
    <div class="exec-row"><span>Current Node</span><strong>${escapeHtml(metadata.lastNodeId || metadata.node || 'n/a')}</strong></div>
    <div class="exec-row"><span>Last Checkpoint</span><strong>${checkpoints}</strong></div>
    <div class="exec-row"><span>Tool Calls</span><strong>${toolCalls}</strong></div>
    <div class="exec-row"><span>Tokens</span><strong>${tokens > 0 ? tokens.toLocaleString() : 'n/a'}</strong></div>
    <div class="exec-row"><span>Retries</span><strong>${retries}</strong></div>
    <div class="exec-row"><span>Attempt</span><strong>${escapeHtml(latestAttempt ? String(latestAttempt.attempt) : 'n/a')}</strong></div>
    <div class="exec-row"><span>Worker</span><strong>${escapeHtml(latestAttempt?.workerId || 'n/a')}</strong></div>
  `;
}

async function loadInterventions(runId) {
  const container = document.getElementById('interventionLog');
  if (!container || !runId) return;
  try {
    const rows = await api.get(`/api/v1/runs/${runId}/interventions`).catch(() => []);
    if (!Array.isArray(rows) || !rows.length) {
      container.innerHTML = '<div class="empty-state"><p>No interventions recorded.</p></div>';
      return;
    }
    container.innerHTML = rows.slice().reverse().map((item) => `
      <div class="intervention-row">
        <div class="intervention-head">
          <span>${escapeHtml(item.action || 'intervention')}</span>
          <span>${formatDate(item.at)}</span>
        </div>
        <div class="intervention-meta">${escapeHtml(item.reason || item.nodeId || item.toolName || '')}</div>
      </div>
    `).join('');
  } catch (e) {
    container.innerHTML = '<div class="empty-state"><p>Failed to load interventions.</p></div>';
  }
}

async function sendIntervention(action, extra = {}) {
  if (!currentRun) return;
  const reason = window.prompt(`Intervention: ${action}. Add reason/context (optional):`, '') || '';
  let payload = { action, reason, ...extra };
  if (action === 'override_router') {
    const route = window.prompt('Route override value:', '') || '';
    payload.route = route;
  }
  if (action === 'inject_tool_result') {
    const toolName = window.prompt('Tool name:', '') || '';
    const result = window.prompt('Injected result:', '') || '';
    payload.toolName = toolName;
    payload.result = result;
  }
  try {
    await api.post(`/api/v1/runs/${currentRun}/interventions`, payload);
    await Promise.all([loadInterventions(currentRun), loadRuns(), selectRun(currentRun)]);
  } catch (e) {
    alert(`Intervention failed: ${e.message || e}`);
  }
}

function renderMessages(events, run) {
  const container = document.getElementById('runMessages');
  if (!container) return;

  // Prefer run.messages (actual LLM conversation) over observer events
  const runMessages = Array.isArray(run?.messages) ? run.messages : [];
  if (runMessages.length > 0) {
    container.innerHTML = runMessages.map(msg => {
      const role = msg.role || 'unknown';
      const roleClass = role === 'user' ? 'user' : (role === 'assistant' ? 'assistant' : 'tool');
      const content = msg.content || '';
      const toolCalls = msg.toolCalls || [];
      let body = '';
      if (content) {
        body = `<pre class="code-block" style="margin: 0; padding: 8px; font-size: 11px; white-space: pre-wrap;">${escapeHtml(content)}</pre>`;
      }
      if (toolCalls.length > 0) {
        body += toolCalls.map(tc => `
          <div style="margin-top: 4px; padding: 6px 8px; background: var(--bg-tertiary); border-radius: 4px; font-size: 11px;">
            <strong>${escapeHtml(tc.name || 'tool')}</strong>
            <pre class="code-block" style="margin: 4px 0 0; padding: 4px; font-size: 10px;">${escapeHtml(JSON.stringify(tc.arguments, null, 2))}</pre>
          </div>
        `).join('');
      }
      return `
        <div class="message-item" style="padding: 12px; border-bottom: 1px solid var(--border-light);">
          <div style="font-size: 12px; color: var(--text-muted); margin-bottom: 4px;">
            <span class="badge" style="background: var(--bg-tertiary); color: var(--text-secondary); font-size: 10px;">${escapeHtml(role)}</span>
            ${msg.name ? `<span style="margin-left: 6px; font-weight: 500;">${escapeHtml(msg.name)}</span>` : ''}
          </div>
          ${body}
        </div>
      `;
    }).join('');
    return;
  }

  // Fallback to observer events
  const messages = events.filter(e =>
    e.type?.includes('message') ||
    e.eventType?.includes('message') ||
    e.type?.includes('generate')
  );

  if (messages.length === 0) {
    container.innerHTML = '<div class="empty-state"><p>No messages</p></div>';
    return;
  }

  container.innerHTML = messages.map(msg => `
    <div class="message-item" style="padding: 12px; border-bottom: 1px solid var(--border-light);">
      <div style="font-size: 12px; color: var(--text-muted); margin-bottom: 4px;">${msg.type || msg.eventType}</div>
      <pre class="code-block" style="margin: 0; padding: 8px; font-size: 11px;">${escapeHtml(JSON.stringify(msg.data || msg.payload, null, 2))}</pre>
    </div>
  `).join('');
}

function renderToolCalls(events, run) {
  const container = document.getElementById('runToolCalls');
  if (!container) return;

  // Prefer run.messages tool calls over observer events
  const runMessages = Array.isArray(run?.messages) ? run.messages : [];
  const msgToolCalls = [];
  runMessages.forEach(msg => {
    if (msg.toolCalls?.length) {
      msg.toolCalls.forEach(tc => msgToolCalls.push(tc));
    }
    if (msg.role === 'tool') {
      msgToolCalls.push({ name: msg.name || 'tool', result: msg.content });
    }
  });

  if (msgToolCalls.length > 0) {
    container.innerHTML = msgToolCalls.map(tc => {
      const isResult = !!tc.result;
      return `
        <div class="tool-call-item" style="padding: 12px; border-bottom: 1px solid var(--border-light);">
          <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 8px;">
            <span style="font-weight: 600;">${escapeHtml(tc.name || 'unknown')}</span>
            <span class="badge" style="background: var(--bg-tertiary); color: var(--text-muted);">${isResult ? 'result' : 'call'}</span>
          </div>
          <pre class="code-block" style="margin: 0; padding: 8px; font-size: 11px;">${escapeHtml(isResult ? tc.result : JSON.stringify(tc.arguments, null, 2))}</pre>
        </div>
      `;
    }).join('');
    return;
  }

  // Fallback to observer events
  const toolCalls = events.filter(e =>
    e.type?.includes('tool') ||
    e.eventType?.includes('tool')
  );

  if (toolCalls.length === 0) {
    container.innerHTML = '<div class="empty-state"><p>No tool calls</p></div>';
    return;
  }

  container.innerHTML = toolCalls.map(tc => {
    const data = tc.data || tc.payload || {};
    const name = data.name || data.toolName || 'unknown';

    return `
      <div class="tool-call-item" style="padding: 12px; border-bottom: 1px solid var(--border-light);">
        <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 8px;">
          <span style="font-weight: 600;">${escapeHtml(name)}</span>
          <span class="badge" style="background: var(--bg-tertiary); color: var(--text-muted);">${tc.type || tc.eventType}</span>
        </div>
        <pre class="code-block" style="margin: 0; padding: 8px; font-size: 11px;">${escapeHtml(JSON.stringify(data, null, 2))}</pre>
      </div>
    `;
  }).join('');
}

// ===== Tools =====
async function loadTools() {
  try {
    const [registry, templates, instances, bundles, providers] = await Promise.all([
      api.get('/api/v1/tools/registry').catch(() => ({ tools: [], bundles: [] })),
      api.get('/api/v1/tools/templates').catch(() => []),
      api.get('/api/v1/tools/instances').catch(() => []),
      api.get('/api/v1/tools/bundles').catch(() => []),
      api.get('/api/v1/integrations/providers').catch(() => []),
    ]);

    const registryTools = Array.isArray(registry?.tools) ? registry.tools : [];
    const catalogTools = Array.isArray(templates) ? templates : [];
    const catalogInstances = Array.isArray(instances) ? instances : [];
    const toolMap = new Map();
    const addTool = (name, description, source, enabled) => {
      const cleanName = String(name || '').trim();
      if (!cleanName) return;
      const cleanSource = String(source || '').trim();
      const cleanDescription = String(description || '').trim();
      const existing = toolMap.get(cleanName);
      if (!existing) {
        toolMap.set(cleanName, {
          name: cleanName,
          description: cleanDescription,
          sources: cleanSource ? [cleanSource] : [],
          enabled: enabled !== false,
        });
        return;
      }
      if (!existing.description && cleanDescription) {
        existing.description = cleanDescription;
      }
      if (cleanSource && !existing.sources.includes(cleanSource)) {
        existing.sources.push(cleanSource);
      }
      if (enabled === true) {
        existing.enabled = true;
      }
    };

    for (const item of registryTools) {
      addTool(item?.name, item?.description, 'built-in', true);
    }
    for (const item of catalogTools) {
      addTool(item?.name, item?.description, 'template', true);
    }
    for (const item of catalogInstances) {
      const source = item?.enabled === false ? 'instance (disabled)' : 'instance';
      addTool(item?.name, '', source, item?.enabled !== false);
    }
    const mergedTools = Array.from(toolMap.values()).sort((a, b) => a.name.localeCompare(b.name));

    const registryBundles = Array.isArray(registry?.bundles) ? registry.bundles : [];
    const catalogBundles = Array.isArray(bundles) ? bundles : [];
    const mergedBundles = [];
    const seenBundles = new Set();
    for (const item of registryBundles) {
      const name = (item?.name || item?.id || item?.Name || item?.ID || '').trim();
      if (!name || seenBundles.has(name)) continue;
      seenBundles.add(name);
      const tools = Array.isArray(item?.tools) ? item.tools : (Array.isArray(item?.Tools) ? item.Tools : []);
      const desc = item?.description || item?.Description || `${tools.length} tools`;
      mergedBundles.push({ name, description: desc, tools });
    }
    for (const item of catalogBundles) {
      const name = (item?.name || item?.id || item?.Name || item?.ID || '').trim();
      if (!name || seenBundles.has(name)) continue;
      seenBundles.add(name);
      const tools = Array.isArray(item?.toolNames) ? item.toolNames : (Array.isArray(item?.tools) ? item.tools : []);
      const desc = item?.description || item?.Description || `${tools.length} tools`;
      mergedBundles.push({ name, description: desc, tools });
    }
    mergedBundles.sort((a, b) => a.name.localeCompare(b.name));

    // Render tools
    const toolsGrid = document.getElementById('toolsGrid');
    if (toolsGrid) {
      if (!mergedTools || mergedTools.length === 0) {
        toolsGrid.innerHTML = '<div class="empty-state"><p>No tools available</p></div>';
      } else {
        toolsGrid.innerHTML = mergedTools.map(tool => `
          <div class="tool-card">
            <div class="tool-card-header">
              <div class="tool-card-icon">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z"/>
                </svg>
              </div>
              <div class="tool-card-name">${escapeHtml(tool.name)}</div>
            </div>
            <div class="tool-card-description">${escapeHtml(tool.description || 'No description')}</div>
            <div class="tool-card-description" style="margin-top: 8px; font-size: 12px;">
              Sources: ${escapeHtml((tool.sources || []).join(', ') || 'unknown')} ${tool.enabled ? '' : '(disabled)'}
            </div>
          </div>
        `).join('');
      }
    }

    // Render bundles
    const bundlesGrid = document.getElementById('bundlesGrid');
    if (bundlesGrid) {
      if (!mergedBundles || mergedBundles.length === 0) {
        bundlesGrid.innerHTML = '<div class="empty-state"><p>No bundles available</p></div>';
      } else {
        bundlesGrid.innerHTML = mergedBundles.map(bundle => `
          <div class="tool-card">
            <div class="tool-card-header">
              <div class="tool-card-icon" style="background: var(--accent-primary); color: white;">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z"/>
                </svg>
              </div>
              <div class="tool-card-name">@${escapeHtml(bundle.name)}</div>
            </div>
            <div class="tool-card-description">${escapeHtml(bundle.description)}</div>
          </div>
        `).join('');
      }
    }

    // Render integrations
    const integrationsGrid = document.getElementById('integrationsGrid');
    if (integrationsGrid) {
      if (!providers || providers.length === 0) {
        integrationsGrid.innerHTML = '<div class="empty-state"><p>No integrations available</p></div>';
      } else {
        integrationsGrid.innerHTML = providers.map(provider => `
          <div class="tool-card">
            <div class="tool-card-header">
              <div class="tool-card-icon" style="background: var(--info-bg); color: var(--info);">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/>
                  <polyline points="15,3 21,3 21,9"/>
                  <line x1="10" y1="14" x2="21" y2="3"/>
                </svg>
              </div>
              <div class="tool-card-name">${escapeHtml(provider.displayName || provider.name)}</div>
            </div>
            <div class="tool-card-description">${escapeHtml(provider.description || 'External integration')}</div>
          </div>
        `).join('');
      }
    }

    await loadToolIntelligence();
  } catch (e) {
    console.error('Failed to load tools:', e);
  }
}

async function loadToolIntelligence() {
  const heatmap = document.getElementById('toolHeatmap');
  const hotspots = document.getElementById('toolHotspots');
  if (!heatmap && !hotspots) return;

  try {
    const intel = await api.get('/api/v1/tools/intelligence?runs=30').catch(() => ({ tools: [], hotspots: [] }));
    const rows = Array.isArray(intel?.tools) ? intel.tools : [];
    if (!rows.length) {
      if (heatmap) heatmap.innerHTML = '<div class="empty-state"><p>No tool call events captured yet.</p></div>';
      if (hotspots) hotspots.innerHTML = '<div class="empty-state"><p>No failure hotspots detected.</p></div>';
      return;
    }
    const maxCalls = Math.max(...rows.map((r) => r.calls), 1);

    if (heatmap) {
      heatmap.innerHTML = rows.slice(0, 12).map((row) => {
        const intensity = Math.max(8, Math.round(((row.calls || 0) / maxCalls) * 100));
        const avgDuration = Number(row.avgDurationMs || 0);
        return `
          <div class="heat-row">
            <div class="heat-head">
              <span class="heat-tool">${escapeHtml(row.name)}</span>
              <span class="heat-stat">${row.calls || 0} calls • avg ${avgDuration}ms</span>
            </div>
            <div class="heat-bar">
              <span class="heat-fill" style="width:${intensity}%"></span>
            </div>
          </div>
        `;
      }).join('');
    }

    if (hotspots) {
      const risky = (Array.isArray(intel?.hotspots) ? intel.hotspots : [])
        .filter((row) => (row.failures || 0) > 0)
        .slice(0, 8);
      if (!risky.length) {
        hotspots.innerHTML = '<div class="empty-state"><p>No failure hotspots detected.</p></div>';
      } else {
        hotspots.innerHTML = risky.map((row) => `
          <div class="hotspot-row">
            <span class="hotspot-tool">${escapeHtml(row.name)}</span>
            <span class="hotspot-rate">${Math.round((row.failureRate || 0) * 100)}%</span>
            <span class="hotspot-meta">${row.failures || 0}/${row.calls || 0} failing calls</span>
          </div>
        `).join('');
      }
    }
  } catch (e) {
    if (heatmap) heatmap.innerHTML = '<div class="empty-state"><p>Tool intelligence unavailable.</p></div>';
    if (hotspots) hotspots.innerHTML = '<div class="empty-state"><p>Hotspot analysis unavailable.</p></div>';
  }
}

// ===== Workflows =====
function syncPlaygroundWorkflowOptions(workflowNames) {
  const select = document.getElementById('playgroundWorkflow');
  if (!select) return;
  const names = Array.isArray(workflowNames) ? workflowNames.filter(Boolean) : [];
  const unique = Array.from(new Set(names));
  const previous = select.value;
  select.innerHTML = '';
  unique.forEach(name => {
    const option = document.createElement('option');
    option.value = name;
    option.textContent = name;
    select.appendChild(option);
  });
  if (unique.length === 0) {
    const option = document.createElement('option');
    option.value = 'basic';
    option.textContent = 'basic';
    select.appendChild(option);
  }
  if (previous && Array.from(select.options).some(o => o.value === previous)) {
    select.value = previous;
  }
}

function setPlaygroundWorkflow(name) {
  const select = document.getElementById('playgroundWorkflow');
  if (!select || !name) return;
  const hasOption = Array.from(select.options).some(o => o.value === name);
  if (!hasOption) {
    const option = document.createElement('option');
    option.value = name;
    option.textContent = name;
    select.appendChild(option);
  }
  select.value = name;
}

function syncGraphWorkflowOptions(workflowNames) {
  const select = document.getElementById('graphWorkflowSelect');
  if (!select) return;
  const names = Array.from(new Set((workflowNames || []).filter(Boolean)));
  if (!names.length) return;
  const previous = selectedGraphWorkflow || select.value;
  select.innerHTML = names.map((name) => `<option value="${escapeHtml(name)}">${escapeHtml(name)}</option>`).join('');
  selectedGraphWorkflow = names.includes(previous) ? previous : names[0];
  select.value = selectedGraphWorkflow;
}

function syncGraphRunOptions(runRows) {
  const select = document.getElementById('graphRunSelect');
  if (!select) return;
  const rows = Array.isArray(runRows) ? runRows : [];
  const options = [`<option value="">None</option>`];
  rows.slice(0, 80).forEach((run) => {
    const runID = runIdOf(run);
    options.push(`<option value="${escapeHtml(runID)}">${escapeHtml(truncate(runID, 20))} • ${escapeHtml(runStatusOf(run))}</option>`);
  });
  select.innerHTML = options.join('');
}

let topologyZoom = 1;
let topologyPanX = 0;
let topologyPanY = 0;
let _topoDrag = { active: false, startX: 0, startY: 0, startPanX: 0, startPanY: 0 };
const ZOOM_MIN = 0.3;
const ZOOM_MAX = 3;
const TOPO_BASE_W = 1200, TOPO_BASE_H = 420;

function zoomTopology(delta) {
  topologyZoom = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, topologyZoom + delta));
  applyTopologyZoom();
}

function resetTopologyView() {
  topologyZoom = 1;
  topologyPanX = 0;
  topologyPanY = 0;
  applyTopologyZoom();
}

function applyTopologyZoom() {
  const svg = document.getElementById('workflowGraphSvg');
  const label = document.getElementById('zoomLevel');
  if (svg) {
    const w = TOPO_BASE_W / topologyZoom, h = TOPO_BASE_H / topologyZoom;
    const ox = (TOPO_BASE_W - w) / 2 + topologyPanX;
    const oy = (TOPO_BASE_H - h) / 2 + topologyPanY;
    svg.setAttribute('viewBox', `${ox} ${oy} ${w} ${h}`);
  }
  if (label) label.textContent = `${Math.round(topologyZoom * 100)}%`;
}

function initTopologyDrag() {
  const wrap = document.getElementById('topologyCanvasWrap');
  if (!wrap) return;

  wrap.addEventListener('mousedown', (e) => {
    if (e.button !== 0) return;
    _topoDrag = { active: true, startX: e.clientX, startY: e.clientY, startPanX: topologyPanX, startPanY: topologyPanY };
    wrap.style.cursor = 'grabbing';
    e.preventDefault();
  });

  window.addEventListener('mousemove', (e) => {
    if (!_topoDrag.active) return;
    const svg = document.getElementById('workflowGraphSvg');
    if (!svg) return;
    const rect = svg.getBoundingClientRect();
    // Convert pixel movement to viewBox units
    const scaleX = (TOPO_BASE_W / topologyZoom) / rect.width;
    const scaleY = (TOPO_BASE_H / topologyZoom) / rect.height;
    topologyPanX = _topoDrag.startPanX - (e.clientX - _topoDrag.startX) * scaleX;
    topologyPanY = _topoDrag.startPanY - (e.clientY - _topoDrag.startY) * scaleY;
    applyTopologyZoom();
  });

  window.addEventListener('mouseup', () => {
    if (_topoDrag.active) {
      _topoDrag.active = false;
      const wrap = document.getElementById('topologyCanvasWrap');
      if (wrap) wrap.style.cursor = 'grab';
    }
  });
}

async function loadWorkflowTopology() {
  const workflowSelect = document.getElementById('graphWorkflowSelect');
  const runSelect = document.getElementById('graphRunSelect');
  const svg = document.getElementById('workflowGraphSvg');
  if (!workflowSelect || !svg) return;
  const workflowName = workflowSelect.value || selectedGraphWorkflow || '';
  if (!workflowName) return;
  selectedGraphWorkflow = workflowName;
  // Reset zoom and pan when switching workflows
  resetTopologyView();
  const runID = runSelect?.value || '';
  try {
    const topology = await api.get(`/api/v1/workflows/${encodeURIComponent(workflowName)}/topology`).catch(() => ({ nodes: [], edges: [] }));
    const nodes = Array.isArray(topology?.nodes) ? topology.nodes : [];
    const edges = Array.isArray(topology?.edges) ? topology.edges : [];
    if (!nodes.length) {
      svg.innerHTML = '';
      return;
    }
    const nodeByID = new Map(nodes.map((n) => [n.id, n]));
    const lines = edges.map((edge) => {
      const from = nodeByID.get(edge.from);
      const to = nodeByID.get(edge.to);
      if (!from || !to) return '';
      return `<line class="graph-edge" x1="${from.x}" y1="${from.y}" x2="${to.x}" y2="${to.y}" />`;
    }).join('');
    const nodeShapes = nodes.map((node) => {
      const kind = String(node.kind || 'node').toLowerCase();
      const label = `${node.executions || 0}x • fail ${(Math.round((node.failureRate || 0) * 100))}% • avg ${node.avgLatencyMs || 0}ms`;
      return `
        <g class="graph-node ${kind}" transform="translate(${node.x},${node.y})">
          <rect x="-72" y="-28" width="144" height="56" rx="11"></rect>
          <text class="graph-node-label" x="0" y="-3">${escapeHtml(node.label || node.id)}</text>
          <text class="graph-node-meta" x="0" y="15">${escapeHtml(label)}</text>
        </g>
      `;
    }).join('');
    svg.innerHTML = `
      <g class="graph-layer edges">${lines}</g>
      <g class="graph-layer nodes">${nodeShapes}</g>
    `;
    if (runID) {
      const checkpoints = await api.get(`/api/v1/runs/${encodeURIComponent(runID)}/checkpoints?limit=200`).catch(() => []);
      (Array.isArray(checkpoints) ? checkpoints : []).forEach((cp) => {
        if (!cp?.nodeId) return;
        const node = nodeByID.get(cp.nodeId);
        if (!node) return;
        const marker = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
        marker.setAttribute('cx', String(node.x + 62));
        marker.setAttribute('cy', String(node.y - 20));
        marker.setAttribute('r', '5');
        marker.setAttribute('class', 'graph-checkpoint');
        marker.setAttribute('data-checkpoint-seq', String(cp.seq || 0));
        marker.setAttribute('data-checkpoint-node', String(cp.nodeId || ''));
        marker.setAttribute('title', `checkpoint ${cp.seq || 0} @ ${cp.nodeId}`);
        marker.style.cursor = 'pointer';
        marker.addEventListener('click', () => {
          currentRun = runID;
          sendIntervention('resume_checkpoint', { checkpoint: cp.seq || 0, nodeId: cp.nodeId || '' });
        });
        svg.appendChild(marker);
      });
    }
  } catch (e) {
    svg.innerHTML = '';
  }
}

async function loadWorkflows() {
  const container = document.getElementById('workflowsGrid');
  if (!container) return;

  try {
    const [bindings, registry] = await Promise.all([
      api.get('/api/v1/workflows').catch(() => []),
      api.get('/api/v1/workflows/registry').catch(() => ({ workflows: [] })),
    ]);
    const bindingRows = Array.isArray(bindings) ? bindings : [];
    const registryRows = Array.isArray(registry?.workflows) ? registry.workflows : [];
    const workflowMap = new Map();
    const descriptions = {
      basic: 'Simple agent workflow',
    };

    registryRows.forEach(item => {
      const name = String(item?.name || '').trim();
      if (!name) return;
      workflowMap.set(name, {
        name,
        description: descriptions[name] || 'Registered workflow',
        binding: null,
      });
    });
    bindingRows.forEach(item => {
      const name = String(item?.workflow || item?.name || '').trim();
      if (!name) return;
      const row = workflowMap.get(name) || {
        name,
        description: descriptions[name] || 'Configured workflow',
        binding: null,
      };
      row.binding = item;
      workflowMap.set(name, row);
    });

    const allWorkflows = Array.from(workflowMap.values()).sort((a, b) => a.name.localeCompare(b.name));
    syncPlaygroundWorkflowOptions(allWorkflows.map(w => w.name));
    syncGraphWorkflowOptions(allWorkflows.map(w => w.name));
    await loadWorkflowTopology();

    if (allWorkflows.length === 0) {
      container.innerHTML = '<div class="empty-state"><p>No workflows configured</p></div>';
      return;
    }

    container.innerHTML = allWorkflows.map(wf => `
      <div class="workflow-card" data-workflow="${escapeHtml(wf.name)}">
        <div class="workflow-card-header">
          <div class="workflow-card-name">${escapeHtml(wf.name)}</div>
          <span class="badge" style="background: var(--success-bg); color: var(--success);">Active</span>
        </div>
        <div class="workflow-card-meta">${escapeHtml(wf.description || 'No description')}</div>
        <div class="workflow-card-meta" style="margin-top: 8px; font-size: 12px;">
          ${(wf.binding?.toolNames?.length || 0)} direct tools • ${(wf.binding?.bundleIds?.length || 0)} bundles
        </div>
      </div>
    `).join('');

    container.querySelectorAll('.workflow-card[data-workflow]').forEach(card => {
      card.addEventListener('click', () => {
        const workflowName = card.getAttribute('data-workflow');
        // Highlight the selected card
        container.querySelectorAll('.workflow-card').forEach(c => c.classList.remove('selected'));
        card.classList.add('selected');
        // Update the graph topology for this workflow (stay on workflows tab)
        selectedGraphWorkflow = workflowName;
        const graphSelect = document.getElementById('graphWorkflowSelect');
        if (graphSelect) {
          graphSelect.value = workflowName;
        }
        loadWorkflowTopology();
      });
    });
  } catch (e) {
    container.innerHTML = '<div class="empty-state"><p>Failed to load workflows</p></div>';
  }
}

// ===== Runtime =====
async function loadRuntime() {
  try {
    const details = await api.get('/api/v1/runtime/details').catch(() => ({
      available: false,
      status: 'unavailable',
      error: 'runtime details unavailable',
      queue: { streamLength: 0, pending: 0, dlqLength: 0 },
      workers: [],
      dlq: [],
    }));
    const queueStats = details.queue || { streamLength: 0, pending: 0, dlqLength: 0 };
    const workers = Array.isArray(details.workers) ? details.workers : [];
    const dlq = Array.isArray(details.dlq) ? details.dlq : [];
    const setText = (id, value) => {
      const el = document.getElementById(id);
      if (el) el.textContent = value;
    };
    const formatErrors = (errs) => {
      if (!errs) return 'none';
      if (typeof errs === 'string') return errs;
      if (typeof errs !== 'object') return String(errs);
      return Object.entries(errs).map(([k, v]) => `${k}: ${v}`).join(' | ') || 'none';
    };

    // Update queue stats
    if (details.available) {
      setText('queue-stream', String(queueStats.streamLength || 0));
      setText('queue-pending', String(queueStats.pending || 0));
      setText('queue-dlq', String(queueStats.dlqLength || 0));
      setText('runtime-status', details.status || 'healthy');
      setText('runtime-errors', formatErrors(details.errors));
    } else {
      setText('queue-stream', '-');
      setText('queue-pending', '-');
      setText('queue-dlq', '-');
      setText('runtime-status', details.status || 'unavailable');
      setText('runtime-errors', details.error || formatErrors(details.errors) || 'runtime service not configured');
    }

    const waveFill = document.getElementById('queueWaveFill');
    const lagLabel = document.getElementById('queueLagLabel');
    const healthLabel = document.getElementById('queueHealthLabel');
    if (waveFill || lagLabel || healthLabel) {
      const streamLen = Number(queueStats.streamLength || 0);
      const pending = Number(queueStats.pending || 0);
      const lagRatio = streamLen > 0 ? Math.min(1, pending / streamLen) : 0;
      const lagPct = Math.round(lagRatio * 100);
      if (waveFill) waveFill.style.width = `${Math.max(6, lagPct)}%`;
      if (lagLabel) lagLabel.textContent = lagPct > 70 ? `High lag ${lagPct}%` : (lagPct > 35 ? `Moderate lag ${lagPct}%` : `Lag nominal ${lagPct}%`);
      if (healthLabel) healthLabel.textContent = details.status || 'unknown';
    }

    // Update workers list
    const workersContainer = document.getElementById('workersList');
    if (workersContainer) {
      if (!workers || workers.length === 0) {
        const msg = details.available ? 'No workers connected' : 'Runtime unavailable';
        workersContainer.innerHTML = `<div class="empty-state"><p>${escapeHtml(msg)}</p></div>`;
      } else {
        workersContainer.innerHTML = workers.map(w => `
          <div class="worker-item">
            <div class="status-indicator ${w.status === 'active' ? 'online' : 'offline'}"></div>
            <span style="flex: 1; font-size: 13px;">${escapeHtml(w.workerId || w.workerID)}</span>
            <span style="font-size: 12px; color: var(--text-muted);">${formatDate(w.lastSeenAt)}</span>
            <button class="btn btn-secondary btn-sm" data-worker-action="inspect" data-worker-id="${escapeHtml(w.workerId || w.workerID)}">Inspect</button>
            <button class="btn btn-secondary btn-sm" data-worker-action="drain" data-worker-id="${escapeHtml(w.workerId || w.workerID)}">Drain</button>
            <button class="btn btn-secondary btn-sm" data-worker-action="disable" data-worker-id="${escapeHtml(w.workerId || w.workerID)}">Disable</button>
          </div>
        `).join('');
        workersContainer.querySelectorAll('[data-worker-action]').forEach((btn) => {
          btn.addEventListener('click', () => {
            const action = btn.getAttribute('data-worker-action');
            const workerId = btn.getAttribute('data-worker-id');
            handleWorkerAction(action, workerId);
          });
        });
      }
    }

    const workerFleet = document.getElementById('workerFleet');
    if (workerFleet) {
      if (!workers || workers.length === 0) {
        workerFleet.innerHTML = '<div class="empty-state"><p>No worker heartbeat</p></div>';
      } else {
        workerFleet.innerHTML = workers.map((w) => `
          <div class="fleet-node ${escapeHtml(String(w.status || 'unknown').toLowerCase())}">
            <span class="fleet-pulse"></span>
            <span class="fleet-id">${escapeHtml(w.workerId || w.workerID || 'worker')}</span>
          </div>
        `).join('');
      }
    }

    // Update DLQ list
    const dlqContainer = document.getElementById('dlqList');
    if (dlqContainer) {
      if (!dlq || dlq.length === 0) {
        const msg = details.available ? 'No failed tasks' : 'Runtime unavailable';
        dlqContainer.innerHTML = `<div class="empty-state"><p>${escapeHtml(msg)}</p></div>`;
      } else {
        dlqContainer.innerHTML = dlq.slice(0, 30).map((item) => `
          <div class="dlq-row">
            <div class="dlq-head">
              <span>${escapeHtml(item?.task?.runId || item?.task?.runID || item?.id || 'run')}</span>
              <button class="btn btn-secondary btn-sm" data-dlq-requeue="${escapeHtml(item?.task?.runId || item?.task?.runID || '')}" data-delivery-id="${escapeHtml(item?.id || '')}">Requeue</button>
            </div>
            <div class="dlq-meta">delivery ${escapeHtml(item?.id || '-')} • received ${formatDate(item?.received)}</div>
          </div>
        `).join('');
        dlqContainer.querySelectorAll('[data-dlq-requeue]').forEach((btn) => {
          btn.addEventListener('click', () => {
            const runId = btn.getAttribute('data-dlq-requeue');
            const deliveryId = btn.getAttribute('data-delivery-id');
            requeueDLQ(runId, deliveryId);
          });
        });
      }
    }
    await loadQueueEvents();
  } catch (e) {
    console.error('Failed to load runtime:', e);
  }
}

async function handleWorkerAction(action, workerId) {
  if (!action || !workerId) return;
  try {
    if (action === 'inspect') {
      const result = await api.get(`/api/v1/runtime/workers/${encodeURIComponent(workerId)}/inspect`);
      alert(`Worker ${workerId}\nstatus=${result?.status || 'unknown'}\nactive tasks=${(result?.tasks || []).length}`);
      return;
    }
    await api.post(`/api/v1/runtime/workers/${encodeURIComponent(workerId)}/${encodeURIComponent(action)}`, {});
    await loadRuntime();
  } catch (e) {
    alert(`Worker action failed: ${e.message || e}`);
  }
}

async function requeueDLQ(runId, deliveryId) {
  try {
    await api.post('/api/v1/runtime/dlq/requeue', { runId, deliveryId });
    await Promise.all([loadRuntime(), loadRuns()]);
  } catch (e) {
    alert(`DLQ requeue failed: ${e.message || e}`);
  }
}

async function loadQueueEvents() {
  const container = document.getElementById('queueEventsList');
  if (!container) return;
  try {
    const rows = await api.get('/api/v1/runtime/queue-events?limit=80').catch(() => []);
    if (!Array.isArray(rows) || !rows.length) {
      container.innerHTML = '<div class="empty-state"><p>No queue events available.</p></div>';
      return;
    }
    container.innerHTML = rows.map((row) => `
      <div class="queue-event-row">
        <span class="queue-event-type">${escapeHtml(row.event || 'event')}</span>
        <span class="queue-event-run">${escapeHtml(truncate(row.runId || '', 20))}</span>
        <span class="queue-event-time">${formatDate(row.at)}</span>
      </div>
    `).join('');
  } catch (e) {
    container.innerHTML = '<div class="empty-state"><p>Queue events unavailable.</p></div>';
  }
}

// ===== Auth Keys =====
async function loadAuthKeys() {
  const container = document.getElementById('authKeysList');
  if (!container) return;

  try {
    const keys = await api.get('/api/v1/auth/keys');

    if (!keys || keys.length === 0) {
      container.innerHTML = '<div class="empty-state"><p>No API keys</p></div>';
      return;
    }

    container.innerHTML = keys.map(key => `
      <div class="worker-item">
        <span style="flex: 1; font-size: 13px; font-family: monospace;">${escapeHtml(key.id)}</span>
        <span class="badge" style="background: var(--bg-tertiary);">${key.role}</span>
      </div>
    `).join('');
  } catch (e) {
    container.innerHTML = '<div class="empty-state"><p>Admin role required to view keys</p></div>';
  }
}

async function loadAuditLogs() {
  const container = document.getElementById('auditLogList');
  if (!container) return;
  try {
    const rows = await api.get('/api/v1/audit/logs?limit=200').catch(() => []);
    if (!Array.isArray(rows) || !rows.length) {
      container.innerHTML = '<div class="empty-state"><p>No audit logs yet.</p></div>';
      return;
    }
    container.innerHTML = rows.map((row) => `
      <div class="audit-row">
        <div class="audit-head">
          <span class="audit-action">${escapeHtml(row.action || 'action')}</span>
          <span class="audit-time">${formatDate(row.createdAt)}</span>
        </div>
        <div class="audit-meta">${escapeHtml(row.resource || '')} • actor ${escapeHtml(row.actorKeyId || 'local')}</div>
        <pre class="audit-payload">${escapeHtml(truncate(row.payload || '', 240))}</pre>
      </div>
    `).join('');
  } catch (e) {
    container.innerHTML = '<div class="empty-state"><p>Failed to load audit logs.</p></div>';
  }
}

// ===== Settings =====
function initSettings() {
  const apiKeyInput = document.getElementById('apiKeyInput');
  const saveApiKeyBtn = document.getElementById('saveApiKey');

  if (apiKeyInput && saveApiKeyBtn) {
    // Load saved key
    const savedKey = localStorage.getItem('devui_api_key');
    if (savedKey) {
      apiKeyInput.value = savedKey;
    }

    saveApiKeyBtn.addEventListener('click', () => {
      const key = apiKeyInput.value.trim();
      if (key) {
        localStorage.setItem('devui_api_key', key);
        alert('API key saved!');
        location.reload();
      } else {
        localStorage.removeItem('devui_api_key');
        alert('API key removed');
        location.reload();
      }
    });
  }
}

// ===== Search =====
function initSearch() {
  const searchInput = document.getElementById('searchRuns');
  if (searchInput) {
    searchInput.addEventListener('input', (e) => {
      const query = e.target.value.toLowerCase();
      document.querySelectorAll('.run-item').forEach(item => {
        const text = item.textContent.toLowerCase();
        item.style.display = text.includes(query) ? 'flex' : 'none';
      });
    });
  }
}

// ===== Command Bar =====
function runControlCommand(raw) {
  const input = String(raw || '').trim();
  const value = input.toLowerCase();
  if (!value) {
    return 'Type a control command.';
  }
  if (value.includes('resume failed')) {
    switchTab('runtime');
    return 'Opened Runtime for requeue operations.';
  }
  if (value.includes('show tool call') || value.includes('tool calls')) {
    switchTab('runs');
    document.querySelectorAll('.run-tab').forEach(t => t.classList.remove('active'));
    document.querySelector('[data-run-tab="tools"]')?.classList.add('active');
    document.querySelectorAll('.run-panel').forEach(p => p.classList.remove('active'));
    document.getElementById('run-tools')?.classList.add('active');
    return 'Opened Runs: Tool Calls.';
  }
  if (value.includes('open runtime') || value.includes('queue') || value.includes('workers')) {
    switchTab('runtime');
    return 'Opened Distributed Runtime.';
  }
  if (value.includes('open tools')) {
    switchTab('tools');
    return 'Opened Tools Hub.';
  }
  if (value.includes('open graph') || value.includes('workflow')) {
    switchTab('workflows');
    return 'Opened Graph Topology.';
  }
  if (value.includes('open playground') || value.includes('test prompt')) {
    switchTab('playground');
    document.getElementById('chatInput')?.focus();
    return 'Opened Playground.';
  }
  if (value.includes('open live') || value === 'live' || value.includes('mission')) {
    switchTab('live');
    return 'Opened Live Agent View.';
  }
  return 'Command not recognized. Try: "resume failed runs", "show tool calls", "open runtime".';
}

function initCommandBar() {
  const input = document.getElementById('commandInput');
  const runButton = document.getElementById('commandRun');
  const result = document.getElementById('commandResult');
  if (!input || !runButton || !result) return;

  const execute = async () => {
    const command = input.value;
    let message = runControlCommand(command);
    try {
      const response = await api.post('/api/v1/commands/execute', { command });
      if (response?.message) {
        message = response.message;
      }
      if (response?.cli) {
        message = `${message}\n${response.cli}`;
      }
    } catch (_) {
      // Keep local fallback message.
    }
    result.textContent = message;
    result.classList.add('active');
    window.setTimeout(() => result.classList.remove('active'), 3600);
  };
  runButton.addEventListener('click', execute);
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      execute();
    }
  });
  document.addEventListener('keydown', (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      input.focus();
      input.select();
    }
  });
}

// ===== Playground =====
let currentInputMode = 'chat';
let playgroundHistoryVisible = false;
let _playgroundHistoryRuns = [];

function setInputMode(mode) {
  const chatMode = document.getElementById('playgroundChatMode');
  const jsonMode = document.getElementById('playgroundJsonMode');
  const chatBtn = document.getElementById('modeChatBtn');
  const jsonBtn = document.getElementById('modeJsonBtn');
  const chatInput = document.getElementById('chatInput');
  const jsonInput = document.getElementById('jsonPayloadInput');

  // Persist input across mode switches
  if (mode === 'json' && currentInputMode === 'chat') {
    const text = chatInput?.value?.trim() || '';
    if (text && jsonInput) {
      try {
        JSON.parse(text);
        jsonInput.value = text;
      } catch (_) {
        jsonInput.value = JSON.stringify({ input: text }, null, 2);
      }
      validateJsonInput();
    }
  } else if (mode === 'chat' && currentInputMode === 'json') {
    const raw = jsonInput?.value?.trim() || '';
    if (raw && chatInput) {
      try {
        const obj = JSON.parse(raw);
        chatInput.value = typeof obj.input === 'string' ? obj.input : raw;
      } catch (_) {
        chatInput.value = raw;
      }
    }
  }

  currentInputMode = mode;
  if (mode === 'json') {
    chatMode && (chatMode.style.display = 'none');
    jsonMode && (jsonMode.style.display = 'block');
    chatBtn?.classList.remove('active');
    jsonBtn?.classList.add('active');
  } else {
    chatMode && (chatMode.style.display = 'flex');
    jsonMode && (jsonMode.style.display = 'none');
    chatBtn?.classList.add('active');
    jsonBtn?.classList.remove('active');
  }
}

// Make setInputMode available globally for onclick handler
window.setInputMode = setInputMode;

function appendChatMessage(role, content, meta) {
  const messages = document.getElementById('chatMessages');
  if (!messages) return;
  const welcome = messages.querySelector('.chat-welcome');
  if (welcome) welcome.remove();
  const roleClass = role === 'user' ? 'user' : 'assistant';
  const safeContent = escapeHtml(content || '');
  const safeMeta = escapeHtml(meta || '');
  const item = document.createElement('div');
  item.className = `chat-bubble ${roleClass}`;
  item.innerHTML = `
    <div class="chat-bubble-role">${role === 'user' ? 'You' : 'Agent'}</div>
    <div class="chat-bubble-content">${safeContent.replace(/\n/g, '<br/>')}</div>
    ${safeMeta ? `<div class="chat-bubble-meta">${safeMeta}</div>` : ''}
  `;
  messages.appendChild(item);
  messages.scrollTop = messages.scrollHeight;
}

async function sendPlaygroundMessage() {
  const input = document.getElementById('chatInput');
  const sendBtn = document.getElementById('sendMessage');
  if (!input || !sendBtn) return;
  const prompt = input.value.trim();
  if (!prompt) return;

  const flowName = document.getElementById('playgroundFlow')?.value || '';
  const workflow = document.getElementById('playgroundWorkflow')?.value || '';
  const tools = Array.from(document.getElementById('playgroundTools')?.selectedOptions || []).map(o => o.value);
  const systemPrompt = document.getElementById('playgroundSystemPrompt')?.value?.trim() || '';

  appendChatMessage('user', prompt);
  input.value = '';
  sendBtn.disabled = true;

  const payload = {
    input: prompt,
    sessionId: playgroundSessionId || undefined,
    flow: flowName || undefined,
    workflow,
    tools,
    systemPrompt,
  };

  // Show streaming progress indicator
  const progressEl = appendStreamingProgress();

  try {
    const response = await streamPlaygroundRun(payload, progressEl);
    removeStreamingProgress(progressEl);
    const status = response?.status || 'completed';
    if (status !== 'completed') {
      appendChatMessage('assistant', response?.error || 'Playground run failed', `status=${status}`);
      return;
    }
    if (response?.sessionId) {
      playgroundSessionId = response.sessionId;
    }
    const meta = [
      response?.provider ? `provider=${response.provider}` : '',
      response?.runId ? `run=${response.runId}` : '',
      response?.sessionId ? `session=${response.sessionId}` : '',
    ].filter(Boolean).join(' • ');
    appendChatMessage('assistant', response?.output || '(empty response)', meta);
    if (playgroundHistoryVisible) loadPlaygroundHistory();
  } catch (e) {
    removeStreamingProgress(progressEl);
    appendChatMessage('assistant', `Request failed: ${e.message || e}`);
  } finally {
    sendBtn.disabled = false;
    sendBtn.innerHTML = `
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20">
        <path d="M22 2L11 13M22 2l-7 20-4-9-9-4 20-7z"/>
      </svg>
    `;
    input.focus();
  }
}

function appendStreamingProgress() {
  const messages = document.getElementById('chatMessages');
  if (!messages) return null;
  const el = document.createElement('div');
  el.className = 'chat-bubble assistant streaming-progress';
  el.innerHTML = `
    <div class="chat-bubble-role">Agent</div>
    <div class="streaming-steps"></div>
    <div class="streaming-spinner">⏳ Processing...</div>
  `;
  messages.appendChild(el);
  messages.scrollTop = messages.scrollHeight;
  return el;
}

function removeStreamingProgress(el) {
  if (el) el.remove();
}

function updateStreamingProgress(el, event) {
  if (!el) return;
  const steps = el.querySelector('.streaming-steps');
  const spinner = el.querySelector('.streaming-spinner');
  if (!steps) return;

  const kind = event.kind || '';
  const status = event.status || '';
  const name = event.name || event.toolName || '';
  let label = '';

  if (kind === 'tool' && status === 'started') {
    label = `🔧 Running tool: ${name}`;
  } else if (kind === 'tool' && status === 'completed') {
    label = `✅ Tool complete: ${name}`;
  } else if (kind === 'tool' && status === 'failed') {
    label = `❌ Tool failed: ${name}`;
  } else if (kind === 'provider' && status === 'started') {
    label = '🤖 Generating response...';
  } else if (kind === 'provider' && status === 'completed') {
    label = '✅ Generation complete';
  } else if (kind === 'run' && status === 'started') {
    label = '▶️ Run started';
  } else if (kind === 'graph' && status === 'started') {
    label = `📊 Step: ${name}`;
  } else if (kind === 'graph' && status === 'completed') {
    label = `✅ Step complete: ${name}`;
  } else if (event.message) {
    label = event.message;
  }

  if (label) {
    const step = document.createElement('div');
    step.className = 'streaming-step';
    step.textContent = label;
    steps.appendChild(step);
  }
  if (spinner) spinner.textContent = '⏳ Processing...';
  const messages = document.getElementById('chatMessages');
  if (messages) messages.scrollTop = messages.scrollHeight;
}

async function streamPlaygroundRun(payload, progressEl) {
  const resp = await fetch('/api/v1/playground/stream', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: JSON.stringify(payload),
  });

  if (!resp.ok) {
    throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
  }

  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let finalResponse = null;

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    // Parse SSE events from buffer
    const lines = buffer.split('\n');
    buffer = lines.pop() || '';

    let eventType = '';
    let dataLines = [];
    for (const line of lines) {
      if (line.startsWith('event: ')) {
        eventType = line.slice(7).trim();
      } else if (line.startsWith('data: ')) {
        dataLines.push(line.slice(6));
      } else if (line === '' && eventType && dataLines.length) {
        try {
          const data = JSON.parse(dataLines.join('\n'));
          if (eventType === 'progress') {
            updateStreamingProgress(progressEl, data);
          } else if (eventType === 'complete') {
            finalResponse = data;
          }
        } catch (_) {}
        eventType = '';
        dataLines = [];
      }
    }
  }

  if (!finalResponse) {
    throw new Error('Stream ended without a completion event');
  }
  return finalResponse;
}

function initPlayground() {
  const sendBtn = document.getElementById('sendMessage');
  const input = document.getElementById('chatInput');
  sendBtn?.addEventListener('click', sendPlaygroundMessage);
  input?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendPlaygroundMessage();
    }
  });

  // JSON payload mode
  const jsonSendBtn = document.getElementById('sendJsonPayload');
  jsonSendBtn?.addEventListener('click', sendJsonPayload);

  const jsonInput = document.getElementById('jsonPayloadInput');
  jsonInput?.addEventListener('input', validateJsonInput);

  // Load graph preview when workflow changes
  const workflowSelect = document.getElementById('playgroundWorkflow');
  workflowSelect?.addEventListener('change', loadPlaygroundGraphPreview);
  loadPlaygroundGraphPreview();

  // Load flows and wire up selector
  const flowSelect = document.getElementById('playgroundFlow');
  flowSelect?.addEventListener('change', onFlowSelected);
  loadFlows();
  loadToolCatalog();
}

let _loadedFlows = [];

async function loadFlows() {
  const select = document.getElementById('playgroundFlow');
  if (!select) return;
  try {
    const [data, configData] = await Promise.all([
      api.get('/api/v1/flows'),
      api.get('/api/v1/config').catch(() => ({})),
    ]);
    const flows = Array.isArray(data?.flows) ? data.flows : [];
    _loadedFlows = flows;
    const defaultFlow = configData?.defaultFlow || '';
    // Keep the (none) option, add flows
    select.innerHTML = '<option value="">(none — configure manually)</option>';
    flows.forEach(f => {
      const opt = document.createElement('option');
      opt.value = f.name;
      opt.textContent = f.name;
      if (f.name === defaultFlow) opt.selected = true;
      select.appendChild(opt);
    });
    // Trigger flow selection if a default is set
    if (defaultFlow && flows.some(f => f.name === defaultFlow)) {
      onFlowSelected();
    }
  } catch (e) {
    // Flows endpoint not available — that's OK
  }
}

async function loadToolCatalog() {
  const select = document.getElementById('playgroundTools');
  if (!select) return;
  try {
    const data = await api.get('/api/v1/tools/catalog');
    const bundles = Array.isArray(data?.bundles) ? data.bundles : [];
    const tools = Array.isArray(data?.tools) ? data.tools : [];
    select.innerHTML = '';

    // Add bundles first as optgroup-style options
    if (bundles.length) {
      const grp = document.createElement('optgroup');
      grp.label = 'Bundles';
      bundles.forEach(b => {
        const opt = document.createElement('option');
        opt.value = b.name;
        opt.textContent = `${b.name}  (${b.tools.length} tools)`;
        opt.title = b.description + '\nTools: ' + b.tools.join(', ');
        if (b.name === '@default') opt.selected = true;
        grp.appendChild(opt);
      });
      select.appendChild(grp);
    }

    // Add individual tools
    if (tools.length) {
      const grp = document.createElement('optgroup');
      grp.label = 'Individual Tools';
      tools.forEach(t => {
        const opt = document.createElement('option');
        opt.value = t.name;
        opt.textContent = t.name;
        opt.title = t.description;
        grp.appendChild(opt);
      });
      select.appendChild(grp);
    }

    // Fallback if nothing loaded
    if (!bundles.length && !tools.length) {
      select.innerHTML = '<option value="@default" selected>@default</option><option value="@all">@all</option>';
    }
  } catch (e) {
    // Fallback to hardcoded defaults
    select.innerHTML = '<option value="@default" selected>@default</option><option value="@all">@all</option>';
  }
}

function onFlowSelected() {
  const select = document.getElementById('playgroundFlow');
  const flowInfo = document.getElementById('playgroundFlowInfo');
  const flowDesc = document.getElementById('playgroundFlowDesc');
  const flowMeta = document.getElementById('playgroundFlowMeta');
  const badge = document.getElementById('configBadge');
  const details = document.getElementById('playgroundConfigDetails');
  const name = select?.value || '';

  // Reset conversation session when flow changes.
  playgroundSessionId = '';
  const chatMessages = document.getElementById('chatMessages');
  if (chatMessages) {
    chatMessages.innerHTML = '<div class="chat-welcome"><p>Send a message to begin a new conversation.</p></div>';
  }

  if (!name) {
    if (flowInfo) flowInfo.style.display = 'none';
    if (badge) { badge.textContent = 'manual'; badge.className = 'config-badge'; }
    // Reset config fields
    const sp = document.getElementById('playgroundSystemPrompt');
    if (sp) sp.value = '';
    const ts = document.getElementById('playgroundTools');
    if (ts) [...ts.options].forEach(o => { o.selected = o.value === '@default'; });
    const ci = document.getElementById('chatInput');
    if (ci) ci.placeholder = 'Send a message...';
    return;
  }

  const f = _loadedFlows.find(fl => fl.name === name);
  if (!f) return;

  // Show flow info bar
  if (flowInfo) flowInfo.style.display = 'block';
  if (flowDesc) flowDesc.textContent = f.description || '';
  if (flowMeta) {
    const tags = [];
    if (f.workflow) tags.push(`<span class="meta-tag">workflow: ${escapeHtml(f.workflow || 'basic')}</span>`);
    if (f.tools?.length) tags.push(`<span class="meta-tag">tools: ${f.tools.map(t => escapeHtml(t)).join(', ')}</span>`);
    flowMeta.innerHTML = tags.join('');
  }

  // Update badge
  if (badge) {
    badge.textContent = name;
    badge.className = 'config-badge flow-active';
  }

  // Collapse config details since flow handles it
  if (details) details.removeAttribute('open');

  // Pre-fill config from flow defaults
  if (f.workflow) {
    const ws = document.getElementById('playgroundWorkflow');
    if (ws) {
      if (![...ws.options].some(o => o.value === f.workflow)) {
        const opt = document.createElement('option');
        opt.value = f.workflow;
        opt.textContent = f.workflow;
        ws.appendChild(opt);
      }
      ws.value = f.workflow;
    }
    loadPlaygroundGraphPreview();
  }

  if (f.systemPrompt) {
    const sp = document.getElementById('playgroundSystemPrompt');
    if (sp) sp.value = f.systemPrompt;
  }

  if (f.tools?.length) {
    const ts = document.getElementById('playgroundTools');
    if (ts) {
      f.tools.forEach(t => {
        if (![...ts.options].some(o => o.value === t)) {
          const opt = document.createElement('option');
          opt.value = t;
          opt.textContent = t;
          ts.appendChild(opt);
        }
      });
      [...ts.options].forEach(o => { o.selected = f.tools.includes(o.value); });
    }
  }

  // If flow has an input example, populate it and update placeholder
  if (f.inputExample) {
    if (currentInputMode === 'json') {
      const ji = document.getElementById('jsonPayloadInput');
      if (ji) ji.value = f.inputExample;
      validateJsonInput();
    } else {
      const ci = document.getElementById('chatInput');
      if (ci) {
        ci.value = f.inputExample;
        ci.placeholder = `Try: ${f.inputExample.substring(0, 60)}...`;
      }
    }
  }
}

async function sendJsonPayload() {
  const textarea = document.getElementById('jsonPayloadInput');
  const sendBtn = document.getElementById('sendJsonPayload');
  const resultDiv = document.getElementById('jsonResultOutput');
  const resultContent = document.getElementById('jsonResultContent');
  if (!textarea || !sendBtn) return;

  const raw = textarea.value.trim();
  if (!raw) return;

  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (e) {
    document.getElementById('jsonValidation').textContent = 'Invalid JSON: ' + e.message;
    document.getElementById('jsonValidation').className = 'json-validation error';
    return;
  }

  sendBtn.disabled = true;
  sendBtn.textContent = 'Running...';

  const flowName = document.getElementById('playgroundFlow')?.value || '';
  const workflow = document.getElementById('playgroundWorkflow')?.value || '';
  const tools = Array.from(document.getElementById('playgroundTools')?.selectedOptions || []).map(o => o.value);
  const systemPrompt = document.getElementById('playgroundSystemPrompt')?.value?.trim() || '';

  const payload = {
    input: typeof parsed === 'string' ? parsed : (parsed.input || JSON.stringify(parsed)),
    sessionId: playgroundSessionId || undefined,
    flow: flowName || undefined,
    workflow,
    tools,
    systemPrompt,
  };

  try {
    const response = await api.post('/api/v1/playground/run', payload);
    // Track session for multi-turn conversation continuity.
    if (response?.sessionId) {
      playgroundSessionId = response.sessionId;
    }
    resultDiv && (resultDiv.style.display = 'block');
    if (resultContent) {
      resultContent.textContent = JSON.stringify(response, null, 2);
    }
    if (playgroundHistoryVisible) loadPlaygroundHistory();
  } catch (e) {
    resultDiv && (resultDiv.style.display = 'block');
    if (resultContent) {
      resultContent.textContent = 'Error: ' + (e.message || e);
    }
  } finally {
    sendBtn.disabled = false;
    sendBtn.textContent = 'Execute';
  }
}

function validateJsonInput() {
  const textarea = document.getElementById('jsonPayloadInput');
  const validation = document.getElementById('jsonValidation');
  if (!textarea || !validation) return;
  const raw = textarea.value.trim();
  if (!raw) { validation.textContent = ''; return; }
  try {
    JSON.parse(raw);
    validation.textContent = '✓ Valid JSON';
    validation.className = 'json-validation valid';
  } catch (e) {
    validation.textContent = '✗ ' + e.message;
    validation.className = 'json-validation error';
  }
}

async function loadPlaygroundGraphPreview() {
  const workflowName = document.getElementById('playgroundWorkflow')?.value || '';
  const previewDiv = document.getElementById('playgroundGraphPreview');
  const canvas = document.getElementById('graphCanvas');
  if (!previewDiv || !canvas || !workflowName) {
    previewDiv && (previewDiv.style.display = 'none');
    return;
  }

  try {
    const data = await api.get(`/api/v1/workflows/${encodeURIComponent(workflowName)}/topology`);
    const nodes = data?.nodes || [];
    const edges = data?.edges || [];
    if (nodes.length <= 1 && edges.length === 0) {
      previewDiv.style.display = 'none';
      return;
    }
    previewDiv.style.display = 'block';
    drawGraphPreview(canvas, nodes, edges);
  } catch (e) {
    previewDiv.style.display = 'none';
  }
}

function drawGraphPreview(canvas, nodes, edges) {
  const ctx = canvas.getContext('2d');
  const dpr = window.devicePixelRatio || 1;
  const w = canvas.clientWidth || 600;
  const h = canvas.clientHeight || 200;
  canvas.width = w * dpr;
  canvas.height = h * dpr;
  ctx.scale(dpr, dpr);
  ctx.clearRect(0, 0, w, h);

  const colors = { agent: '#3b82f6', tool: '#10b981', router: '#f59e0b' };
  const nodeMap = {};
  const padding = 20;
  const nodeW = 100, nodeH = 36;

  // Scale positions to fit canvas
  let maxX = 0, maxY = 0;
  nodes.forEach(n => { maxX = Math.max(maxX, n.x + nodeW); maxY = Math.max(maxY, n.y + nodeH); });
  const scaleX = maxX > 0 ? (w - padding * 2) / maxX : 1;
  const scaleY = maxY > 0 ? (h - padding * 2) / maxY : 1;
  const scale = Math.min(scaleX, scaleY, 1);

  nodes.forEach(n => {
    nodeMap[n.id] = { x: padding + n.x * scale, y: padding + n.y * scale, node: n };
  });

  // Draw edges
  ctx.lineWidth = 1.5;
  edges.forEach(e => {
    const from = nodeMap[e.from];
    const to = nodeMap[e.to];
    if (!from || !to) return;
    ctx.beginPath();
    ctx.strokeStyle = e.conditional ? '#f59e0b' : '#6b7280';
    if (e.conditional) ctx.setLineDash([4, 4]);
    else ctx.setLineDash([]);
    ctx.moveTo(from.x + nodeW * scale / 2, from.y + nodeH * scale / 2);
    ctx.lineTo(to.x + nodeW * scale / 2, to.y + nodeH * scale / 2);
    ctx.stroke();
    ctx.setLineDash([]);
  });

  // Draw nodes
  nodes.forEach(n => {
    const pos = nodeMap[n.id];
    if (!pos) return;
    const nw = nodeW * scale, nh = nodeH * scale;
    ctx.fillStyle = colors[n.kind] || '#6b7280';
    ctx.globalAlpha = 0.15;
    ctx.fillRect(pos.x, pos.y, nw, nh);
    ctx.globalAlpha = 1;
    ctx.strokeStyle = colors[n.kind] || '#6b7280';
    ctx.lineWidth = 2;
    ctx.strokeRect(pos.x, pos.y, nw, nh);
    ctx.fillStyle = 'var(--text, #e5e7eb)';
    ctx.font = `${Math.max(10, 12 * scale)}px system-ui, sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillStyle = colors[n.kind] || '#6b7280';
    ctx.fillText(n.label || n.id, pos.x + nw / 2, pos.y + nh / 2);
  });
}

// ===== Playground History =====
function togglePlaygroundHistory() {
  playgroundHistoryVisible = !playgroundHistoryVisible;
  const panel = document.getElementById('playgroundHistory');
  const container = document.querySelector('.playground-container');
  const btn = document.getElementById('toggleHistoryBtn');
  if (panel) panel.style.display = playgroundHistoryVisible ? '' : 'none';
  if (container) container.classList.toggle('with-history', playgroundHistoryVisible);
  if (btn) btn.classList.toggle('active', playgroundHistoryVisible);
  if (playgroundHistoryVisible) loadPlaygroundHistory();
}

async function loadPlaygroundHistory() {
  const list = document.getElementById('historyList');
  if (!list) return;
  list.innerHTML = '<div class="history-empty">Loading...</div>';
  try {
    const runs = await api.get('/api/v1/runs?limit=50&offset=0');
    if (!Array.isArray(runs) || runs.length === 0) {
      list.innerHTML = '<div class="history-empty">No conversations yet</div>';
      _playgroundHistoryRuns = [];
      return;
    }
    // Group runs by session_id. Runs without session go individually.
    const sessions = new Map();
    const standalone = [];
    for (const run of runs) {
      const sid = run.sessionId || '';
      if (sid) {
        if (!sessions.has(sid)) sessions.set(sid, []);
        sessions.get(sid).push(run);
      } else {
        standalone.push(run);
      }
    }

    _playgroundHistoryRuns = runs;
    list.innerHTML = '';

    // Render session groups
    for (const [sid, sessionRuns] of sessions) {
      const latest = sessionRuns[0];
      const count = sessionRuns.length;
      const preview = latest.input || latest.output || '(no content)';
      const time = formatRelativeTime(latest.createdAt);
      const status = latest.status || 'completed';
      const item = document.createElement('div');
      item.className = 'history-item' + (sid === playgroundSessionId ? ' active' : '');
      item.onclick = () => restoreSession(sid, sessionRuns);
      item.innerHTML = `
        <div class="history-item-title">${escapeHtml(truncate(preview, 60))}</div>
        <div class="history-item-meta">
          <span class="status-dot ${status}"></span>
          <span>${count} message${count !== 1 ? 's' : ''}</span>
          <span>•</span>
          <span>${time}</span>
        </div>
      `;
      list.appendChild(item);
    }

    // Render standalone runs
    for (const run of standalone) {
      const preview = run.input || run.output || '(no content)';
      const time = formatRelativeTime(run.createdAt);
      const status = run.status || 'completed';
      const item = document.createElement('div');
      item.className = 'history-item';
      item.onclick = () => restoreSingleRun(run);
      item.innerHTML = `
        <div class="history-item-title">${escapeHtml(truncate(preview, 60))}</div>
        <div class="history-item-meta">
          <span class="status-dot ${status}"></span>
          <span>1 message</span>
          <span>•</span>
          <span>${time}</span>
        </div>
      `;
      list.appendChild(item);
    }
  } catch (e) {
    list.innerHTML = `<div class="history-empty">Failed to load: ${escapeHtml(e.message || String(e))}</div>`;
  }
}

async function restoreSession(sessionId, sessionRuns) {
  // Mark active in UI
  document.querySelectorAll('.history-item').forEach(el => el.classList.remove('active'));
  event?.target?.closest?.('.history-item')?.classList.add('active');

  // Set session ID for continuity
  playgroundSessionId = sessionId;

  // Clear chat and show messages from this session
  const messages = document.getElementById('chatMessages');
  if (!messages) return;
  messages.innerHTML = '';

  // Load full run details for each run in the session (most recent first, reverse for chronological)
  const orderedRuns = [...sessionRuns].reverse();
  for (const run of orderedRuns) {
    if (run.input) appendChatMessage('user', run.input);
    if (run.output) {
      const meta = [
        run.provider ? `provider=${run.provider}` : '',
        run.runId ? `run=${run.runId}` : '',
      ].filter(Boolean).join(' • ');
      appendChatMessage('assistant', run.output, meta);
    }
  }

  // Switch to chat mode if in JSON mode
  if (currentInputMode !== 'chat') setInputMode('chat');
  document.getElementById('chatInput')?.focus();
}

function restoreSingleRun(run) {
  document.querySelectorAll('.history-item').forEach(el => el.classList.remove('active'));
  event?.target?.closest?.('.history-item')?.classList.add('active');

  playgroundSessionId = '';
  const messages = document.getElementById('chatMessages');
  if (!messages) return;
  messages.innerHTML = '';

  if (run.input) appendChatMessage('user', run.input);
  if (run.output) {
    const meta = [
      run.provider ? `provider=${run.provider}` : '',
      run.runId ? `run=${run.runId}` : '',
    ].filter(Boolean).join(' • ');
    appendChatMessage('assistant', run.output, meta);
  }
  if (currentInputMode !== 'chat') setInputMode('chat');
  document.getElementById('chatInput')?.focus();
}

function startNewConversation() {
  playgroundSessionId = '';
  const messages = document.getElementById('chatMessages');
  if (messages) {
    messages.innerHTML = '<div class="chat-welcome"><p>Send a message to begin a new conversation.</p></div>';
  }
  // Deselect all history items
  document.querySelectorAll('.history-item').forEach(el => el.classList.remove('active'));
  document.getElementById('chatInput')?.focus();
}

function truncate(str, len) {
  if (!str) return '';
  return str.length > len ? str.substring(0, len) + '…' : str;
}

function formatRelativeTime(dateStr) {
  if (!dateStr) return '';
  const now = Date.now();
  const then = new Date(dateStr).getTime();
  const diffSec = Math.floor((now - then) / 1000);
  if (diffSec < 60) return 'just now';
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`;
  if (diffSec < 604800) return `${Math.floor(diffSec / 86400)}d ago`;
  return new Date(dateStr).toLocaleDateString();
}

// ===== Scheduler =====
async function loadCronJobs() {
  const list = document.getElementById('cronJobList');
  if (!list) return;
  try {
    const jobs = await api.get('/api/v1/cron/jobs');
    if (!Array.isArray(jobs) || jobs.length === 0) {
      list.innerHTML = '<div class="empty-state"><p>No scheduled jobs yet</p></div>';
      return;
    }
    list.innerHTML = jobs.map(j => `
      <div class="cron-job-row" style="display:flex;align-items:center;justify-content:space-between;padding:12px;border-bottom:1px solid var(--border-color);">
        <div style="flex:1;">
          <strong>${escapeHtml(j.name)}</strong>
          <span style="margin-left:8px;font-size:12px;color:var(--text-muted);font-family:monospace;">${escapeHtml(j.cronExpr)}</span>
          ${j.config?.workflow ? `<span class="badge">${escapeHtml(j.config.workflow)}</span>` : ''}
          ${!j.enabled ? '<span class="badge" style="background:var(--accent-warning);color:#000;">paused</span>' : ''}
        </div>
        <div style="font-size:12px;color:var(--text-muted);text-align:right;min-width:160px;">
          <div>Runs: ${j.runCount || 0}</div>
          ${j.lastRun ? `<div>Last: ${new Date(j.lastRun).toLocaleString()}</div>` : ''}
          ${j.nextRun ? `<div>Next: ${new Date(j.nextRun).toLocaleString()}</div>` : ''}
          ${j.lastError ? `<div style="color:var(--accent-danger);">${escapeHtml(j.lastError)}</div>` : ''}
        </div>
        <div style="display:flex;gap:4px;margin-left:12px;">
          <button class="btn btn-secondary btn-sm" onclick="triggerCronJob('${escapeHtml(j.name)}')" title="Trigger now">▶</button>
          <button class="btn btn-secondary btn-sm" onclick="toggleCronJobEnabled('${escapeHtml(j.name)}', ${!j.enabled})" title="${j.enabled ? 'Pause' : 'Resume'}">${j.enabled ? '⏸' : '▶'}</button>
          <button class="btn btn-secondary btn-sm" onclick="deleteCronJob('${escapeHtml(j.name)}')" title="Delete" style="color:var(--accent-danger);">✕</button>
        </div>
      </div>
    `).join('');
  } catch (e) {
    list.innerHTML = `<div class="empty-state"><p>Scheduler not available</p></div>`;
  }
}

function toggleCronForm() {
  const form = document.getElementById('cronJobForm');
  if (form) form.style.display = form.style.display === 'none' ? 'block' : 'none';
}

async function createCronJob() {
  const name = document.getElementById('cronJobName')?.value?.trim();
  const cronExpr = document.getElementById('cronJobExpr')?.value?.trim();
  const input = document.getElementById('cronJobInput')?.value?.trim();
  const workflow = document.getElementById('cronJobWorkflow')?.value || '';
  const systemPrompt = document.getElementById('cronJobSystemPrompt')?.value?.trim() || '';
  if (!name || !cronExpr || !input) {
    alert('Name, cron expression, and input are required');
    return;
  }
  try {
    await api.post('/api/v1/cron/jobs', {
      name,
      cronExpr,
      config: { input, workflow, systemPrompt, tools: ['@default'] },
    });
    toggleCronForm();
    document.getElementById('cronJobName').value = '';
    document.getElementById('cronJobExpr').value = '';
    document.getElementById('cronJobInput').value = '';
    document.getElementById('cronJobSystemPrompt').value = '';
    loadCronJobs();
  } catch (e) {
    alert('Failed to create job: ' + (e.message || e));
  }
}

async function triggerCronJob(name) {
  try {
    const resp = await api.post(`/api/v1/cron/jobs/${encodeURIComponent(name)}/trigger`, {});
    alert(`Job triggered. Status: ${resp?.status || 'unknown'}\nOutput: ${(resp?.output || '').substring(0, 200)}`);
    loadCronJobs();
  } catch (e) {
    alert('Trigger failed: ' + (e.message || e));
  }
}

async function toggleCronJobEnabled(name, enabled) {
  try {
    await api.request(`/api/v1/cron/jobs/${encodeURIComponent(name)}`, { method: 'PATCH', body: JSON.stringify({ enabled }) });
    loadCronJobs();
  } catch (e) {
    alert('Update failed: ' + (e.message || e));
  }
}

async function deleteCronJob(name) {
  if (!confirm(`Delete scheduled job "${name}"?`)) return;
  try {
    await api.request(`/api/v1/cron/jobs/${encodeURIComponent(name)}`, { method: 'DELETE' });
    loadCronJobs();
  } catch (e) {
    alert('Delete failed: ' + (e.message || e));
  }
}

// Make scheduler functions available globally
window.toggleCronForm = toggleCronForm;
window.createCronJob = createCronJob;
window.triggerCronJob = triggerCronJob;
window.toggleCronJobEnabled = toggleCronJobEnabled;
window.deleteCronJob = deleteCronJob;
window.loadCronJobs = loadCronJobs;

// ===== Event Buttons =====
function initButtons() {
  document.getElementById('refreshRuns')?.addEventListener('click', loadRuns);
  document.getElementById('refreshTools')?.addEventListener('click', loadTools);
  document.getElementById('refreshWorkflows')?.addEventListener('click', loadWorkflows);
  document.getElementById('refreshKeys')?.addEventListener('click', loadAuthKeys);
  document.getElementById('refreshRuntime')?.addEventListener('click', loadRuntime);
  document.getElementById('refreshQueueEvents')?.addEventListener('click', loadQueueEvents);
  document.getElementById('refreshAudit')?.addEventListener('click', loadAuditLogs);
  document.getElementById('refreshTopology')?.addEventListener('click', loadWorkflowTopology);
  document.getElementById('zoomIn')?.addEventListener('click', () => zoomTopology(0.2));
  document.getElementById('zoomOut')?.addEventListener('click', () => zoomTopology(-0.2));
  document.getElementById('zoomReset')?.addEventListener('click', () => { resetTopologyView(); });
  // Mouse wheel zoom on topology canvas
  document.getElementById('topologyCanvasWrap')?.addEventListener('wheel', (e) => {
    e.preventDefault();
    zoomTopology(e.deltaY < 0 ? 0.1 : -0.1);
  }, { passive: false });
  initTopologyDrag();
  document.getElementById('graphWorkflowSelect')?.addEventListener('change', () => {
    selectedGraphWorkflow = document.getElementById('graphWorkflowSelect')?.value || '';
    loadWorkflowTopology();
  });
  document.getElementById('graphRunSelect')?.addEventListener('change', loadWorkflowTopology);
  document.querySelectorAll('[data-intervention]').forEach((btn) => {
    btn.addEventListener('click', () => {
      const action = btn.getAttribute('data-intervention');
      if (action) sendIntervention(action);
    });
  });
}

// ===== SSE =====
function initSSE() {
  const key = localStorage.getItem('devui_api_key');
  const qs = key ? `?api_key=${encodeURIComponent(key)}` : '';

  try {
    const source = new EventSource(`/api/v1/stream/events${qs}`);

    source.onmessage = () => {
      loadDashboard();
      loadRecentActivity();
      if (currentRun) {
        selectRun(currentRun);
      }
      if (document.getElementById('tab-runtime')?.classList.contains('active')) {
        loadRuntime();
      }
      if (document.getElementById('tab-tools')?.classList.contains('active')) {
        loadToolIntelligence();
      }
      if (document.getElementById('tab-audit')?.classList.contains('active')) {
        loadAuditLogs();
      }
    };

    source.onerror = () => {
      console.log('SSE connection error, will retry...');
    };
  } catch (e) {
    console.log('SSE not available');
  }
}

// ===== Trace Tree =====
function renderTraceTree(run, events) {
  const container = document.getElementById('runTraceTree');
  if (!container) return;

  const messages = Array.isArray(run?.messages) ? run.messages : [];
  if (messages.length === 0) {
    container.innerHTML = '<div class="empty-state"><p>No trace data available</p></div>';
    return;
  }

  let stepNum = 0;
  const steps = messages.map(msg => {
    stepNum++;
    const role = msg.role || 'unknown';
    let iconClass = 'user';
    let label = 'User Input';
    let detail = msg.content || '';

    if (role === 'assistant') {
      iconClass = 'model';
      label = 'Model Response';
      if (msg.toolCalls?.length) {
        label = `Model → ${msg.toolCalls.length} tool call(s)`;
        detail = msg.toolCalls.map(tc =>
          `${tc.name || 'tool'}(${JSON.stringify(tc.arguments || {})})`
        ).join('\n');
        if (msg.content) detail = msg.content + '\n\n' + detail;
      }
    } else if (role === 'tool') {
      iconClass = 'tool-call';
      label = `Tool Result: ${msg.name || 'tool'}`;
    } else if (role === 'system') {
      iconClass = 'model';
      label = 'System Prompt';
    }

    return `
      <div class="trace-node">
        <div class="trace-step" onclick="this.querySelector('.trace-step-detail')?.classList.toggle('expanded')">
          <div class="trace-step-icon ${iconClass}">${stepNum}</div>
          <div class="trace-step-info">
            <div class="trace-step-title">${escapeHtml(label)}</div>
            <div class="trace-step-detail">${escapeHtml(truncate(detail, 200))}</div>
          </div>
          <div class="trace-step-meta">${role}</div>
        </div>
      </div>
    `;
  });

  // Add run summary at top
  const dur = run.durationMs || run.duration || '';
  const provider = run.provider || '';
  const summary = `
    <div style="margin-bottom:12px; padding:10px; background:var(--bg-tertiary); border-radius:6px; font-size:12px;">
      <strong>Run:</strong> ${escapeHtml(truncate(run.id || '', 24))}
      ${provider ? ` • <strong>Provider:</strong> ${escapeHtml(provider)}` : ''}
      ${dur ? ` • <strong>Duration:</strong> ${dur}ms` : ''}
      • <strong>Steps:</strong> ${messages.length}
    </div>
  `;

  container.innerHTML = summary + steps.join('');
}

// ===== Actions Tab =====
let actionsCache = [];
let selectedAction = null;
let actionsTypeFilter = 'all';

async function loadActions() {
  try {
    const resp = await api.get('/api/v1/reflect');
    actionsCache = Array.isArray(resp?.actions) ? resp.actions : [];
    renderActionsList();
  } catch (e) {
    console.error('Failed to load actions:', e);
    document.getElementById('actionsList').innerHTML =
      '<div class="empty-state"><p>Failed to load actions</p></div>';
  }
}

function renderActionsList() {
  const container = document.getElementById('actionsList');
  if (!container) return;

  const search = (document.getElementById('actionsSearch')?.value || '').toLowerCase();
  const filtered = actionsCache.filter(a => {
    if (actionsTypeFilter !== 'all' && a.type !== actionsTypeFilter) return false;
    if (search && !a.name.toLowerCase().includes(search) && !(a.description || '').toLowerCase().includes(search)) return false;
    return true;
  });

  if (filtered.length === 0) {
    container.innerHTML = '<div class="empty-state"><p>No matching actions</p></div>';
    return;
  }

  container.innerHTML = filtered.map(a => `
    <div class="action-item ${selectedAction?.key === a.key ? 'selected' : ''}"
         data-key="${escapeHtml(a.key)}" onclick="selectAction('${escapeHtml(a.key)}')">
      <span class="action-type-badge ${a.type}">${a.type}</span>
      <div class="action-item-info">
        <div class="action-item-name">${escapeHtml(a.name)}</div>
        <div class="action-item-desc">${escapeHtml(a.description || '')}</div>
      </div>
    </div>
  `).join('');
}

function filterActions() {
  renderActionsList();
}

function filterActionsByType(type) {
  actionsTypeFilter = type;
  document.querySelectorAll('#actionsTypeFilter .toggle-btn').forEach(b => {
    b.classList.toggle('active', b.getAttribute('data-type') === type);
  });
  renderActionsList();
}

function selectAction(key) {
  selectedAction = actionsCache.find(a => a.key === key);
  if (!selectedAction) return;

  renderActionsList(); // Update selection highlight

  document.getElementById('actionsEmptyDetail').style.display = 'none';
  document.getElementById('actionDetail').style.display = 'block';

  // Header
  document.getElementById('actionDetailType').textContent = selectedAction.type;
  document.getElementById('actionDetailType').className = 'action-type-badge ' + selectedAction.type;
  document.getElementById('actionDetailName').textContent = selectedAction.name;
  document.getElementById('actionDetailDesc').textContent = selectedAction.description || '';

  // Schema display
  const schema = selectedAction.inputSchema;
  document.getElementById('actionSchemaRaw').textContent = schema
    ? JSON.stringify(schema, null, 2)
    : 'No schema defined';

  // Generate form
  renderSchemaForm(schema);

  // Reset result
  document.getElementById('actionResultCard').style.display = 'none';
  document.getElementById('actionTraceCard').style.display = 'none';

  // Pre-fill JSON editor
  if (schema?.properties) {
    const example = {};
    const props = schema.properties;
    for (const [k, v] of Object.entries(props)) {
      if (v.type === 'string') example[k] = selectedAction.metadata?.inputExample || '';
      else if (v.type === 'number') example[k] = 0;
      else if (v.type === 'boolean') example[k] = false;
      else example[k] = '';
    }
    document.getElementById('actionJsonInput').value = JSON.stringify(example, null, 2);
  } else {
    document.getElementById('actionJsonInput').value = '{}';
  }
}

function renderSchemaForm(schema) {
  const container = document.getElementById('actionFormFields');
  if (!container) return;

  if (!schema?.properties) {
    container.innerHTML = `
      <div class="schema-field">
        <label>input <span class="field-type">string</span></label>
        <textarea class="textarea" data-field="input" rows="4" placeholder="Enter input..."></textarea>
      </div>
    `;
    return;
  }

  const required = Array.isArray(schema.required) ? schema.required : [];
  const props = schema.properties;
  const html = [];

  for (const [fieldName, fieldDef] of Object.entries(props)) {
    const isRequired = required.includes(fieldName);
    const fieldType = fieldDef.type || 'string';
    const desc = fieldDef.description || '';
    const enumVals = fieldDef.enum;

    let inputHtml = '';
    if (enumVals) {
      inputHtml = `<select class="select" data-field="${escapeHtml(fieldName)}">
        <option value="">-- select --</option>
        ${enumVals.map(v => `<option value="${escapeHtml(v)}">${escapeHtml(v)}</option>`).join('')}
      </select>`;
    } else if (fieldType === 'boolean') {
      inputHtml = `<select class="select" data-field="${escapeHtml(fieldName)}">
        <option value="">-- select --</option>
        <option value="true">true</option>
        <option value="false">false</option>
      </select>`;
    } else if (fieldType === 'number' || fieldType === 'integer') {
      inputHtml = `<input class="input" data-field="${escapeHtml(fieldName)}" type="number" placeholder="0" />`;
    } else if (fieldType === 'string' && (fieldName === 'input' || fieldName === 'content' || fieldName === 'data' || fieldName === 'text')) {
      inputHtml = `<textarea class="textarea" data-field="${escapeHtml(fieldName)}" rows="4" placeholder="Enter ${escapeHtml(fieldName)}..."></textarea>`;
    } else {
      inputHtml = `<input class="input" data-field="${escapeHtml(fieldName)}" type="text" placeholder="Enter ${escapeHtml(fieldName)}..." />`;
    }

    html.push(`
      <div class="schema-field">
        <label>
          ${escapeHtml(fieldName)}
          <span class="field-type">${escapeHtml(fieldType)}</span>
          ${isRequired ? '<span class="field-required">required</span>' : ''}
        </label>
        ${desc ? `<div class="field-desc">${escapeHtml(desc)}</div>` : ''}
        ${inputHtml}
      </div>
    `);
  }

  container.innerHTML = html.join('');
}

function setActionInputMode(mode) {
  const formBtn = document.getElementById('actionFormModeBtn');
  const jsonBtn = document.getElementById('actionJsonModeBtn');
  const formFields = document.getElementById('actionFormFields');
  const jsonEditor = document.getElementById('actionJsonEditor');
  const jsonInput = document.getElementById('actionJsonInput');

  // Persist input across mode switches
  if (mode === 'json' && formFields && formFields.style.display !== 'none') {
    const formData = collectFormInput();
    if (jsonInput && Object.keys(formData).length > 0) {
      jsonInput.value = JSON.stringify(formData, null, 2);
    }
  } else if (mode === 'form' && jsonEditor && jsonEditor.style.display !== 'none') {
    if (jsonInput) {
      try {
        const obj = JSON.parse(jsonInput.value);
        const fields = document.querySelectorAll('#actionFormFields [data-field]');
        fields.forEach(f => {
          const name = f.getAttribute('data-field');
          if (obj[name] !== undefined) f.value = String(obj[name]);
        });
      } catch (_) { /* ignore parse errors */ }
    }
  }

  if (mode === 'form') {
    formBtn?.classList.add('active');
    jsonBtn?.classList.remove('active');
    if (formFields) formFields.style.display = '';
    if (jsonEditor) jsonEditor.style.display = 'none';
  } else {
    formBtn?.classList.remove('active');
    jsonBtn?.classList.add('active');
    if (formFields) formFields.style.display = 'none';
    if (jsonEditor) jsonEditor.style.display = '';
  }
}

function collectFormInput() {
  const fields = document.querySelectorAll('#actionFormFields [data-field]');
  const input = {};
  fields.forEach(f => {
    const name = f.getAttribute('data-field');
    let val = f.value;
    if (f.type === 'number' && val !== '') val = Number(val);
    if (f.tagName === 'SELECT' && val === 'true') val = true;
    if (f.tagName === 'SELECT' && val === 'false') val = false;
    if (val !== '' && val !== null) input[name] = val;
  });
  return input;
}

async function runAction() {
  if (!selectedAction) return;

  const btn = document.getElementById('runActionBtn');
  btn.disabled = true;
  btn.textContent = 'Running…';

  // Determine input based on mode
  const jsonEditor = document.getElementById('actionJsonEditor');
  const isJsonMode = jsonEditor && jsonEditor.style.display !== 'none';
  let input;
  if (isJsonMode) {
    try {
      input = JSON.parse(document.getElementById('actionJsonInput').value);
    } catch (e) {
      alert('Invalid JSON: ' + e.message);
      btn.disabled = false;
      btn.textContent = 'Run';
      return;
    }
  } else {
    input = collectFormInput();
  }

  try {
    const resp = await api.post('/api/v1/actions/run', {
      key: selectedAction.key,
      input: input,
    });

    // Show result
    const card = document.getElementById('actionResultCard');
    card.style.display = '';

    const statusEl = document.getElementById('actionResultStatus');
    statusEl.textContent = resp.status || 'unknown';
    statusEl.className = 'status-badge badge status-' + (resp.status === 'success' || resp.status === 'completed' ? 'completed' : 'failed');

    document.getElementById('actionResultDuration').textContent =
      resp.duration ? `${resp.duration}ms` : '';

    const output = resp.error || resp.output;
    document.getElementById('actionResultOutput').textContent =
      typeof output === 'string' ? output : JSON.stringify(output, null, 2);

    // Show trace for flow runs
    if (resp.runId) {
      const traceCard = document.getElementById('actionTraceCard');
      traceCard.style.display = '';
      try {
        const run = await api.get(`/api/v1/runs/${resp.runId}`);
        const traceContainer = document.getElementById('actionTraceContent');
        const messages = Array.isArray(run?.messages) ? run.messages : [];
        if (messages.length > 0) {
          let stepNum = 0;
          traceContainer.innerHTML = messages.map(msg => {
            stepNum++;
            const role = msg.role || 'unknown';
            let iconClass = role === 'assistant' ? 'model' : (role === 'tool' ? 'tool-call' : 'user');
            let label = role === 'assistant' ? 'Model' : (role === 'tool' ? `Tool: ${msg.name || ''}` : 'User');
            let detail = msg.content || '';
            if (msg.toolCalls?.length) {
              label += ` → ${msg.toolCalls.length} call(s)`;
              detail = msg.toolCalls.map(tc => `${tc.name}(${JSON.stringify(tc.arguments || {})})`).join('\n');
            }
            return `<div class="trace-node">
              <div class="trace-step" onclick="this.querySelector('.trace-step-detail')?.classList.toggle('expanded')">
                <div class="trace-step-icon ${iconClass}">${stepNum}</div>
                <div class="trace-step-info">
                  <div class="trace-step-title">${escapeHtml(label)}</div>
                  <div class="trace-step-detail">${escapeHtml(truncate(detail, 200))}</div>
                </div>
                <div class="trace-step-meta">${role}</div>
              </div>
            </div>`;
          }).join('');
        } else {
          traceContainer.innerHTML = '<p style="color:var(--text-muted);font-size:12px;">No trace steps</p>';
        }
      } catch (e) {
        document.getElementById('actionTraceContent').textContent = 'Failed to load trace: ' + e.message;
      }
    }
  } catch (e) {
    const card = document.getElementById('actionResultCard');
    card.style.display = '';
    document.getElementById('actionResultStatus').textContent = 'error';
    document.getElementById('actionResultStatus').className = 'status-badge badge status-failed';
    document.getElementById('actionResultOutput').textContent = e.message || String(e);
  } finally {
    btn.disabled = false;
    btn.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><polygon points="5,3 19,12 5,21"/></svg> Run`;
  }
}

// ===== Bootstrap =====
(async function init() {
  initTheme();
  initNavigation();
  initSettings();
  initSearch();
  initCommandBar();
  initPlayground();
  initButtons();

  // Load all data
  await Promise.all([
    loadDashboard(),
    loadRecentActivity(),
    loadRuns(),
    loadTools(),
    loadWorkflows(),
    loadRuntime(),
    loadAuthKeys(),
    loadAuditLogs(),
  ]);

  initSSE();
})();
