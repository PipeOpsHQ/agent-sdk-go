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
function initNavigation() {
  document.querySelectorAll('[data-tab]').forEach(btn => {
    btn.addEventListener('click', () => {
      const tab = btn.getAttribute('data-tab');

      // Update nav
      document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
      btn.classList.add('active');

      // Update tabs
      document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
      document.getElementById(`tab-${tab}`)?.classList.add('active');
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
    const [metrics, runtime, registry] = await Promise.all([
      api.get('/api/v1/metrics/summary').catch(() => ({})),
      api.get('/api/v1/runtime/details').catch(() => ({ available: false, status: 'unavailable' })),
      api.get('/api/v1/tools/registry').catch(() => ({ toolCount: 0 })),
    ]);

    // Update metrics
    const completed = metrics.completed || metrics.total_completed || 0;
    const running = metrics.running || metrics.in_progress || 0;
    const failed = metrics.failed || metrics.total_failed || 0;
    const tools = registry.toolCount || metrics.tools || metrics.active_tools || 0;

    document.getElementById('metric-completed').textContent = completed;
    document.getElementById('metric-running').textContent = running;
    document.getElementById('metric-failed').textContent = failed;
    document.getElementById('metric-tools').textContent = tools;

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
  if (!container) return;

  try {
    const recentRuns = await api.get('/api/v1/runs?limit=10');

    if (!recentRuns || recentRuns.length === 0) {
      container.innerHTML = '<div class="empty-state"><p>No recent activity</p></div>';
      return;
    }

    container.innerHTML = recentRuns.map(run => `
      <div class="activity-item">
        <div class="activity-dot ${run.status || 'pending'}"></div>
        <div class="activity-content">
          <div class="activity-title">${escapeHtml(truncate(run.runId, 24))}</div>
          <div class="activity-meta">${run.provider || 'unknown'} • ${formatDate(run.updatedAt)}</div>
        </div>
      </div>
    `).join('');
  } catch (e) {
    container.innerHTML = '<div class="empty-state"><p>Failed to load activity</p></div>';
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
      <div class="run-item ${currentRun === run.runId ? 'selected' : ''}" data-run-id="${run.runId}">
        <div class="run-item-status ${run.status || 'pending'}"></div>
        <div class="run-item-content">
          <div class="run-item-id">${escapeHtml(truncate(run.runId, 20))}</div>
          <div class="run-item-meta">${run.status || 'pending'} • ${formatDate(run.updatedAt)}</div>
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
    const [run, events] = await Promise.all([
      api.get(`/api/v1/runs/${runId}`),
      api.get(`/api/v1/runs/${runId}/events?limit=500`).catch(() => []),
    ]);

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
    renderTimeline(events);

    // Update messages
    renderMessages(events);

    // Update tool calls
    renderToolCalls(events);

  } catch (e) {
    console.error('Failed to load run details:', e);
  }
}

function renderTimeline(events) {
  const container = document.getElementById('runTimeline');
  if (!container) return;

  if (!events || events.length === 0) {
    container.innerHTML = '<div class="empty-state"><p>No events</p></div>';
    return;
  }

  container.innerHTML = events.slice(0, 50).map(event => {
    const type = event.type || event.eventType || 'event';
    const statusClass = type.includes('error') ? 'error' :
                       type.includes('complete') ? 'success' : '';

    return `
      <div class="timeline-item ${statusClass}">
        <div class="timeline-time">${formatDate(event.timestamp)}</div>
        <div class="timeline-title">${escapeHtml(type)}</div>
        <div class="timeline-content">${escapeHtml(truncate(JSON.stringify(event.data || event.payload || {}), 100))}</div>
      </div>
    `;
  }).join('');
}

function renderMessages(events) {
  const container = document.getElementById('runMessages');
  if (!container) return;

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

function renderToolCalls(events) {
  const container = document.getElementById('runToolCalls');
  if (!container) return;

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
    const [registry, templates, bundles, providers] = await Promise.all([
      api.get('/api/v1/tools/registry').catch(() => ({ tools: [], bundles: [] })),
      api.get('/api/v1/tools/templates').catch(() => []),
      api.get('/api/v1/tools/bundles').catch(() => []),
      api.get('/api/v1/integrations/providers').catch(() => []),
    ]);

    const registryTools = Array.isArray(registry?.tools) ? registry.tools : [];
    const catalogTools = Array.isArray(templates) ? templates : [];
    const mergedTools = [];
    const seenTools = new Set();
    for (const item of registryTools) {
      const name = (item?.name || '').trim();
      if (!name || seenTools.has(name)) continue;
      seenTools.add(name);
      mergedTools.push({ name, description: item?.description || '' });
    }
    for (const item of catalogTools) {
      const name = (item?.name || '').trim();
      if (!name || seenTools.has(name)) continue;
      seenTools.add(name);
      mergedTools.push({ name, description: item?.description || '' });
    }
    mergedTools.sort((a, b) => a.name.localeCompare(b.name));

    const registryBundles = Array.isArray(registry?.bundles) ? registry.bundles : [];
    const catalogBundles = Array.isArray(bundles) ? bundles : [];
    const mergedBundles = [];
    const seenBundles = new Set();
    for (const item of registryBundles) {
      const name = (item?.name || item?.id || '').trim();
      if (!name || seenBundles.has(name)) continue;
      seenBundles.add(name);
      const tools = Array.isArray(item?.tools) ? item.tools : [];
      mergedBundles.push({ name, description: item?.description || `${tools.length} tools`, tools });
    }
    for (const item of catalogBundles) {
      const name = (item?.name || item?.id || '').trim();
      if (!name || seenBundles.has(name)) continue;
      seenBundles.add(name);
      const tools = Array.isArray(item?.tools) ? item.tools : [];
      mergedBundles.push({ name, description: item?.description || `${tools.length} tools`, tools });
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
  } catch (e) {
    console.error('Failed to load tools:', e);
  }
}

// ===== Workflows =====
async function loadWorkflows() {
  const container = document.getElementById('workflowsGrid');
  if (!container) return;

  try {
    const workflows = await api.get('/api/v1/workflows').catch(() => []);

    // Also get built-in workflow names if available
    const builtinWorkflows = ['basic', 'secops'].map(name => ({
      name,
      description: name === 'basic' ? 'Simple agent workflow' : 'Security operations workflow',
    }));

    const allWorkflows = [...builtinWorkflows];

    if (allWorkflows.length === 0) {
      container.innerHTML = '<div class="empty-state"><p>No workflows configured</p></div>';
      return;
    }

    container.innerHTML = allWorkflows.map(wf => `
      <div class="workflow-card">
        <div class="workflow-card-header">
          <div class="workflow-card-name">${escapeHtml(wf.name)}</div>
          <span class="badge" style="background: var(--success-bg); color: var(--success);">Active</span>
        </div>
        <div class="workflow-card-meta">${escapeHtml(wf.description || 'No description')}</div>
      </div>
    `).join('');
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

    // Update queue stats
    if (details.available) {
      document.getElementById('queue-stream').textContent = queueStats.streamLength || 0;
      document.getElementById('queue-pending').textContent = queueStats.pending || 0;
      document.getElementById('queue-dlq').textContent = queueStats.dlqLength || 0;
      document.getElementById('runtime-status').textContent = details.status || 'healthy';
      const errors = details.errors ? JSON.stringify(details.errors) : '';
      document.getElementById('runtime-errors').textContent = errors || 'none';
    } else {
      document.getElementById('queue-stream').textContent = '-';
      document.getElementById('queue-pending').textContent = '-';
      document.getElementById('queue-dlq').textContent = '-';
      document.getElementById('runtime-status').textContent = details.status || 'unavailable';
      document.getElementById('runtime-errors').textContent = details.error || 'runtime service not configured';
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
        dlqContainer.innerHTML = `<pre class="code-block">${escapeHtml(JSON.stringify(dlq, null, 2))}</pre>`;
      }
    }
  } catch (e) {
    console.error('Failed to load runtime:', e);
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

// ===== Event Buttons =====
function initButtons() {
  document.getElementById('refreshRuns')?.addEventListener('click', loadRuns);
  document.getElementById('refreshTools')?.addEventListener('click', loadTools);
  document.getElementById('refreshWorkflows')?.addEventListener('click', loadWorkflows);
  document.getElementById('refreshKeys')?.addEventListener('click', loadAuthKeys);
  document.getElementById('refreshRuntime')?.addEventListener('click', loadRuntime);
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
    };

    source.onerror = () => {
      console.log('SSE connection error, will retry...');
    };
  } catch (e) {
    console.log('SSE not available');
  }
}

// ===== Bootstrap =====
(async function init() {
  initTheme();
  initNavigation();
  initSettings();
  initSearch();
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
  ]);

  initSSE();
})();
