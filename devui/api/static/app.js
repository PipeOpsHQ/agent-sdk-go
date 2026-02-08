const api = {
  get: async (path) => {
    const res = await fetch(path, { headers: authHeaders() });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
  },
};

function authHeaders() {
  const key = localStorage.getItem("devui_api_key");
  if (!key) return {};
  return { "X-API-Key": key };
}

function json(nodeId, value) {
  const node = document.getElementById(nodeId);
  if (!node) return;
  node.textContent = JSON.stringify(value, null, 2);
}

function setTabs() {
  const buttons = document.querySelectorAll("nav button[data-tab]");
  buttons.forEach((btn) => {
    btn.addEventListener("click", () => {
      buttons.forEach((b) => b.classList.remove("active"));
      btn.classList.add("active");
      const tab = btn.getAttribute("data-tab");
      document.querySelectorAll(".tab").forEach((el) => el.classList.remove("active"));
      document.getElementById(`tab-${tab}`).classList.add("active");
    });
  });
}

async function loadMetrics() {
  try {
    const m = await api.get("/api/v1/metrics/summary");
    json("metrics", m);
  } catch (e) {
    json("metrics", { error: String(e) });
  }
}

async function loadRuns() {
  const runsList = document.getElementById("runsList");
  runsList.innerHTML = "";
  try {
    const runs = await api.get("/api/v1/runs?limit=100");
    runs.forEach((run) => {
      const li = document.createElement("li");
      const btn = document.createElement("button");
      btn.textContent = `${run.runId} | ${run.status} | ${run.provider}`;
      btn.onclick = () => loadRunDetail(run.runId);
      li.appendChild(btn);
      runsList.appendChild(li);
    });
  } catch (e) {
    const li = document.createElement("li");
    li.textContent = String(e);
    runsList.appendChild(li);
  }
}

async function loadRunDetail(runId) {
  try {
    const [run, events, checkpoints, attempts] = await Promise.all([
      api.get(`/api/v1/runs/${runId}`),
      api.get(`/api/v1/runs/${runId}/events?limit=500`),
      api.get(`/api/v1/runs/${runId}/checkpoints?limit=50`),
      api.get(`/api/v1/runtime/runs/${runId}/attempts?limit=50`).catch(() => []),
    ]);
    json("runDetail", run);
    json("runEvents", events);
    json("runCheckpoints", checkpoints);
    json("runAttempts", attempts);
  } catch (e) {
    json("runDetail", { error: String(e) });
    json("runEvents", { error: String(e) });
    json("runCheckpoints", { error: String(e) });
    json("runAttempts", { error: String(e) });
  }
}

async function loadCatalog() {
  try {
    const [templates, instances, bundles, providers] = await Promise.all([
      api.get("/api/v1/tools/templates"),
      api.get("/api/v1/tools/instances"),
      api.get("/api/v1/tools/bundles"),
      api.get("/api/v1/integrations/providers").catch(() => []),
    ]);
    json("toolTemplates", templates);
    json("toolInstances", instances);
    json("toolBundles", bundles);
    json("integrationProviders", providers);
  } catch (e) {
    json("toolTemplates", { error: String(e) });
    json("toolInstances", { error: String(e) });
    json("toolBundles", { error: String(e) });
    json("integrationProviders", { error: String(e) });
  }
}

async function loadWorkflows() {
  try {
    const bindings = await api.get("/api/v1/workflows");
    json("workflowBindings", bindings);
  } catch (e) {
    json("workflowBindings", { error: String(e) });
  }
}

async function loadAuthKeys() {
  try {
    const keys = await api.get("/api/v1/auth/keys");
    json("authKeys", keys);
  } catch (e) {
    json("authKeys", { error: String(e), hint: "admin role required" });
  }
}

async function loadRuntime() {
  try {
    const [queueStats, workers, dlq] = await Promise.all([
      api.get("/api/v1/runtime/queues").catch(() => ({ unavailable: true })),
      api.get("/api/v1/runtime/workers").catch(() => []),
      api.get("/api/v1/runtime/dlq?limit=100").catch(() => []),
    ]);
    json("runtimeQueue", queueStats);
    json("runtimeWorkers", workers);
    json("runtimeDLQ", dlq);
  } catch (e) {
    json("runtimeQueue", { error: String(e) });
    json("runtimeWorkers", { error: String(e) });
    json("runtimeDLQ", { error: String(e) });
  }
}

function setupButtons() {
  document.getElementById("refreshRuns")?.addEventListener("click", loadRuns);
  document.getElementById("refreshTools")?.addEventListener("click", loadCatalog);
  document.getElementById("refreshWorkflows")?.addEventListener("click", loadWorkflows);
  document.getElementById("refreshKeys")?.addEventListener("click", loadAuthKeys);
  document.getElementById("refreshRuntime")?.addEventListener("click", loadRuntime);
}

function openSSE() {
  const key = localStorage.getItem("devui_api_key");
  const qs = key ? `?api_key=${encodeURIComponent(key)}` : "";
  const source = new EventSource(`/api/v1/stream/events${qs}`);
  source.onmessage = () => {
    loadMetrics();
    loadRuntime();
  };
}

(async function bootstrap() {
  setTabs();
  setupButtons();
  await Promise.all([loadMetrics(), loadRuns(), loadCatalog(), loadWorkflows(), loadAuthKeys(), loadRuntime()]);
  openSSE();
})();
