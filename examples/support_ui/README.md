# Support UI Example

This example runs a small customer-support chat UI and calls the framework Playground API.

It does **not** depend on a server-side named flow; it sends its own support configuration
(workflow, prompt, tools, skills, and guardrails) in every request.

## Run

From this folder:

```bash
go run . --addr=127.0.0.1:8090 --api-base=http://127.0.0.1:7070
```

Optional API key:

```bash
go run . --addr=127.0.0.1:8090 --api-base=http://127.0.0.1:7070 --api-key="<DEVUI_API_KEY>"
```

Then open `http://127.0.0.1:8090`.
