package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/devui"
	"github.com/PipeOpsHQ/agent-sdk-go/flow"
)

const supportFlowName = "support-ui-example"

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "UI listen address")
	apiBase := flag.String("api-base", "http://127.0.0.1:7070", "Framework DevUI API base URL")
	apiKey := flag.String("api-key", "", "Optional DevUI API key")
	startAPI := flag.Bool("start-api", false, "Start embedded DevUI API (SDK-style) with support flow")
	apiAddr := flag.String("api-addr", "127.0.0.1:7070", "Embedded DevUI API listen address when --start-api=true")
	flag.Parse()

	if *startAPI {
		registerSupportFlow()
		go func() {
			if err := devui.Start(context.Background(), devui.Options{
				Addr:        strings.TrimSpace(*apiAddr),
				DefaultFlow: supportFlowName,
			}); err != nil {
				log.Fatalf("embedded DevUI API failed: %v", err)
			}
		}()
		*apiBase = "http://" + strings.TrimSpace(*apiAddr)
	}

	html := supportHTML(strings.TrimRight(*apiBase, "/"), strings.TrimSpace(*apiKey))
	upstream, err := url.Parse(strings.TrimRight(*apiBase, "/"))
	if err != nil {
		log.Fatalf("invalid --api-base URL: %v", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	})
	mux.Handle("/api/", proxy)

	log.Printf("Support UI listening on http://%s", *addr)
	log.Printf("Connected API base: %s", *apiBase)
	if *startAPI {
		log.Printf("Embedded DevUI API started on http://%s with default flow %q", *apiAddr, supportFlowName)
	}
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func registerSupportFlow() {
	flow.MustRegister(&flow.Definition{
		Name:        supportFlowName,
		Description: "Support-focused flow for customer troubleshooting with research + docs tooling.",
		Workflow:    "summary-memory",
		Tools:       []string{"@default", "@network", "@docs"},
		Skills:      []string{"document-manager", "research-planner", "pdf-reporting"},
		SystemPrompt: `You are a customer support engineer for PipeOps.
- Be empathetic and concise
- Continue to the next troubleshooting step without unnecessary confirmation
- Use research and documentation tools when needed
- Provide clear expected outcomes for each step`,
		InputExample: "A customer webhook integration intermittently fails with 429. Diagnose and provide remediation steps.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{"type": "string", "description": "Customer support issue or request."},
			},
			"required": []string{"input"},
		},
	})
}

func supportHTML(apiBase, apiKey string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>PipeOps Support Demo</title>
  <style>
    html,body{height:100%%}
    body{font-family:ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,Arial;margin:0;background:#f4f7fb;color:#10243d;overflow:hidden}
    .app{height:100vh;overflow:hidden;padding:16px}
    .wrap{max-width:980px;height:100%%;margin:0 auto}
    .card{height:100%%;display:flex;flex-direction:column;background:#fff;border:1px solid #dbe5f0;border-radius:12px;box-shadow:0 4px 10px rgba(16,36,61,.06);overflow:hidden}
    .head{padding:14px 16px;border-bottom:1px solid #dbe5f0;display:flex;justify-content:space-between;align-items:center}
    .head h1{font-size:16px;margin:0}
    .meta{font-size:12px;color:#4b647f}
    .chat{flex:1;min-height:0;overflow:auto;padding:12px;background:linear-gradient(180deg,#f9fcff,#f3f8ff)}
    .bubble{border:1px solid #dbe5f0;border-radius:10px;padding:10px;margin-bottom:10px;background:#f8fbff}
    .bubble.user{background:#eef6ff;border-color:#b7d4f5}
    .bubble.assistant{background:#ffffff}
    .role{font-size:11px;font-weight:700;color:#4b647f;margin-bottom:4px}
    .content{white-space:pre-wrap;line-height:1.45;font-size:14px}
    .content h3{font-size:14px;margin:8px 0 6px}
    .content ul{margin:6px 0 8px 18px}
    .content code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;background:#eef2f8;border:1px solid #d7e2f0;padding:1px 4px;border-radius:4px;font-size:12px}
    .meta2{margin-top:6px;font-size:11px;color:#6f86a0}
    .status{margin-top:6px;font-size:11px;color:#55708f}
    .compose{display:flex;gap:8px;padding:10px;border-top:1px solid #dbe5f0;flex-shrink:0;background:#fff}
    textarea{flex:1;min-height:44px;max-height:140px;border:1px solid #c7d7e8;border-radius:8px;padding:10px;resize:vertical}
    button{background:#1f6fca;color:#fff;border:none;border-radius:8px;padding:10px 14px;cursor:pointer}
    button:disabled{opacity:.6;cursor:not-allowed}
    .hint{font-size:12px;color:#607993;padding:8px 12px}
    @media (max-width: 768px){
      .app{padding:8px}
      .head{padding:10px 12px}
      .compose{padding:8px}
      .chat{padding:10px}
    }
  </style>
</head>
<body>
  <div class="app">
  <div class="wrap">
    <div class="card">
      <div class="head">
        <h1>Customer Support Assistant (Example)</h1>
        <div class="meta">Flow: %s • Prompt: support-agent@v1</div>
      </div>
      <div class="hint">This demo calls Framework Playground API with support mode defaults and session continuity.</div>
      <div id="chat" class="chat"></div>
      <div class="compose">
        <textarea id="input" placeholder="Ask a support question..."></textarea>
        <button id="send">Send</button>
      </div>
    </div>
  </div>
  </div>
<script>
  const API_BASE = "/api";
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
    flow: %q,
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
    const contentEl = row.querySelector('.content');
    if (role === 'assistant') {
      contentEl.innerHTML = renderMarkdownLite(content);
    } else {
      contentEl.textContent = content;
    }
    chat.appendChild(row);
    chat.scrollTop = chat.scrollHeight;
  }

  function esc(s) {
    return String(s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  }

  function renderMarkdownLite(text) {
    let html = esc(String(text || ''));
    html = html.replace(/^###\s+(.+)$/gm, '<h3>$1</h3>');
    html = html.replace(/^[-*]\s+(.+)$/gm, '<li>$1</li>');
    html = html.replace(/(<li>.*<\/li>\n?)+/g, (m) => '<ul>' + m + '</ul>');
    html = html.replace(new RegExp('\\x60([^\\x60]+)\\x60', 'g'), '<code>$1</code>');
    html = html.replace(/\n{2,}/g, '</p><p>');
    html = '<p>' + html + '</p>';
    return html;
  }

  async function send() {
    const input = document.getElementById("input");
    const btn = document.getElementById("send");
    const text = input.value.trim();
    if (!text) return;
    input.value = "";
    input.style.height = '44px';
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
      btn.textContent = 'Sending...';
      const resp = await fetch(API_BASE + "/v1/playground/stream", {
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
      streamBubble.innerHTML = '<div class="role">Agent</div><div class="content"></div><div class="status">Starting...</div><div class="meta2">streaming...</div>';
      document.getElementById("chat").appendChild(streamBubble);
      const streamContent = streamBubble.querySelector('.content');
      const streamStatus = streamBubble.querySelector('.status');
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
                streamContent.innerHTML = renderMarkdownLite(streaming);
              } else if (eventType === "progress") {
                const label = [evt.kind, evt.status, evt.name || evt.toolName || ''].filter(Boolean).join(' • ');
                if (streamStatus && label) streamStatus.textContent = label;
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
      btn.textContent = 'Send';
    }
  }

  document.getElementById("send").addEventListener("click", send);
  document.getElementById("input").addEventListener("input", (e) => {
    const el = e.target;
    el.style.height = '44px';
    el.style.height = Math.min(el.scrollHeight, 140) + 'px';
  });
  document.getElementById("input").addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  });
  push("assistant", "Hi! I am your support assistant demo. Ask me anything about PipeOps services and I will respond with support-mode defaults.");
</script>
</body>
</html>`, supportFlowName, apiKey, supportFlowName)
}
