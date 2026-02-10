package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "UI listen address")
	apiBase := flag.String("api-base", "http://127.0.0.1:7070", "Framework DevUI API base URL")
	apiKey := flag.String("api-key", "", "Optional DevUI API key")
	flag.Parse()

	html := supportHTML(strings.TrimRight(*apiBase, "/"), strings.TrimSpace(*apiKey))

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	})

	log.Printf("Support UI listening on http://%s", *addr)
	log.Printf("Connected API base: %s", *apiBase)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func supportHTML(apiBase, apiKey string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>PipeOps Support Demo</title>
  <style>
    body{font-family:ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,Arial;margin:0;background:#f4f7fb;color:#10243d}
    .wrap{max-width:900px;margin:0 auto;padding:16px}
    .card{background:#fff;border:1px solid #dbe5f0;border-radius:12px;box-shadow:0 4px 10px rgba(16,36,61,.06)}
    .head{padding:14px 16px;border-bottom:1px solid #dbe5f0;display:flex;justify-content:space-between;align-items:center}
    .head h1{font-size:16px;margin:0}
    .meta{font-size:12px;color:#4b647f}
    .chat{height:62vh;overflow:auto;padding:12px}
    .bubble{border:1px solid #dbe5f0;border-radius:10px;padding:10px;margin-bottom:10px;background:#f8fbff}
    .bubble.user{background:#eef6ff;border-color:#b7d4f5}
    .role{font-size:11px;font-weight:700;color:#4b647f;margin-bottom:4px}
    .content{white-space:pre-wrap;line-height:1.45}
    .meta2{margin-top:6px;font-size:11px;color:#6f86a0}
    .compose{display:flex;gap:8px;padding:10px;border-top:1px solid #dbe5f0}
    textarea{flex:1;min-height:44px;max-height:140px;border:1px solid #c7d7e8;border-radius:8px;padding:10px;resize:vertical}
    button{background:#1f6fca;color:#fff;border:none;border-radius:8px;padding:10px 14px;cursor:pointer}
    button:disabled{opacity:.6;cursor:not-allowed}
    .hint{font-size:12px;color:#607993;padding:8px 12px}
  </style>
</head>
<body>
  <div class="wrap">
    <div class="card">
      <div class="head">
        <h1>Customer Support Assistant (Example)</h1>
        <div class="meta">Support config: summary-memory + support-agent@v1</div>
      </div>
      <div class="hint">This demo calls Framework Playground API with support mode defaults and session continuity.</div>
      <div id="chat" class="chat"></div>
      <div class="compose">
        <textarea id="input" placeholder="Ask a support question..."></textarea>
        <button id="send">Send</button>
      </div>
    </div>
  </div>
<script>
  const API_BASE = %q;
  const API_KEY = %q;
  let sessionId = "";
  const history = [];
  const RAG_CONTEXT = [
    "PipeOps resources:",
    "- https://pipeops.io",
    "- https://www.crunchbase.com/organization/pipeops",
    "- https://www.linkedin.com/company/pipeops/",
    "Use web_search/web_scraper to gather current details when needed."
  ].join("\n");

  const SUPPORT_CONFIG = {
    workflow: "summary-memory",
    promptRef: "support-agent@v1",
    promptInput: { department: "customer-support", style: "empathetic and concise" },
    tools: ["@default", "@network", "@docs"],
    skills: ["document-manager", "research-planner", "pdf-reporting"],
    guardrails: ["prompt_injection", "pii_filter", "secret_guard", "content_filter"]
  };

  function headers() {
    const h = {"Content-Type":"application/json"};
    if (API_KEY) h["X-API-Key"] = API_KEY;
    return h;
  }

  function push(role, content, meta="") {
    const chat = document.getElementById("chat");
    const row = document.createElement("div");
    row.className = "bubble " + role;
    row.innerHTML = '<div class="role">' + (role === 'user' ? 'You' : 'Agent') + '</div><div class="content"></div>' + (meta ? '<div class="meta2">' + meta + '</div>' : '');
    row.querySelector('.content').textContent = content;
    chat.appendChild(row);
    chat.scrollTop = chat.scrollHeight;
  }

  async function send() {
    const input = document.getElementById("input");
    const btn = document.getElementById("send");
    const text = input.value.trim();
    if (!text) return;
    input.value = "";
    push("user", text);
    history.push({role:"user", content:text});
    btn.disabled = true;

    const payload = {
      input: RAG_CONTEXT + "\n\nCustomer request:\n" + text,
      sessionId: sessionId || undefined,
      history,
      ...SUPPORT_CONFIG
    };

    try {
      const resp = await fetch(API_BASE + "/api/v1/playground/stream", {
        method: "POST",
        headers: headers(),
        body: JSON.stringify(payload)
      });
      if (!resp.ok) throw new Error("HTTP " + resp.status);
      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let data = null;
      let streaming = "";
      const streamBubble = document.createElement("div");
      streamBubble.className = "bubble assistant";
      streamBubble.innerHTML = '<div class="role">Agent</div><div class="content"></div><div class="meta2">streaming...</div>';
      document.getElementById("chat").appendChild(streamBubble);
      const streamContent = streamBubble.querySelector('.content');
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";
        let eventType = "";
        let payloadLines = [];
        for (const line of lines) {
          if (line.startsWith("event: ")) eventType = line.slice(7).trim();
          else if (line.startsWith("data: ")) payloadLines.push(line.slice(6));
          else if (line === "") {
            if (!eventType || !payloadLines.length) continue;
            try {
              const evt = JSON.parse(payloadLines.join("\n"));
              if (eventType === "delta") {
                streaming += String(evt.text || "");
                streamContent.textContent = streaming;
              } else if (eventType === "complete") {
                data = evt;
              }
            } catch (_) {}
            eventType = "";
            payloadLines = [];
          }
        }
      }
      if (streamBubble) streamBubble.remove();
      if (!data) throw new Error("stream ended without completion");
      if (data.sessionId) sessionId = data.sessionId;
      const meta = [data.provider ? ('provider=' + data.provider) : "", data.runId ? ('run=' + data.runId) : "", data.sessionId ? ('session=' + data.sessionId) : ""].filter(Boolean).join(" • ");
      push("assistant", data.output || data.error || "(empty response)", meta);
      history.push({role:"assistant", content:data.output || data.error || ""});
      if (history.length > 24) history.splice(0, history.length - 24);
    } catch (e) {
      push("assistant", "Request failed: " + (e.message || e));
    } finally {
      btn.disabled = false;
    }
  }

  document.getElementById("send").addEventListener("click", send);
  document.getElementById("input").addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  });
  push("assistant", "Hi! I am your support assistant demo. Ask me anything about PipeOps services and I will respond with support-mode defaults.");
</script>
</body>
</html>`, apiBase, apiKey)
}
