package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	cronpkg "github.com/PipeOpsHQ/agent-sdk-go/framework/runtime/cron"
)

func runCronCLI(_ context.Context, args []string) {
	if len(args) == 0 {
		printCronUsage()
		os.Exit(1)
	}
	addr := "127.0.0.1:7070"
	apiKey := ""

	var filtered []string
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--addr="):
			addr = strings.TrimPrefix(arg, "--addr=")
		case strings.HasPrefix(arg, "--api-key="):
			apiKey = strings.TrimPrefix(arg, "--api-key=")
		default:
			filtered = append(filtered, arg)
		}
	}
	if len(filtered) == 0 {
		printCronUsage()
		os.Exit(1)
	}

	base := "http://" + addr
	client := &cronCLIClient{base: base, apiKey: apiKey}

	switch filtered[0] {
	case "list", "ls":
		client.list()
	case "add":
		client.add(filtered[1:])
	case "remove", "rm":
		client.remove(filtered[1:])
	case "trigger":
		client.trigger(filtered[1:])
	case "enable":
		client.setEnabled(filtered[1:], true)
	case "disable":
		client.setEnabled(filtered[1:], false)
	case "get":
		client.get(filtered[1:])
	default:
		log.Fatalf("unknown cron command %q", filtered[0])
	}
}

type cronCLIClient struct {
	base   string
	apiKey string
}

func (c *cronCLIClient) doRequest(method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed (is the UI server running at %s?): %w", c.base, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func (c *cronCLIClient) list() {
	data, err := c.doRequest(http.MethodGet, "/api/v1/cron/jobs", nil)
	if err != nil {
		log.Fatal(err)
	}
	var jobs []cronpkg.Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		log.Fatalf("parse response: %v", err)
	}
	if len(jobs) == 0 {
		fmt.Println("No scheduled jobs.")
		return
	}
	fmt.Printf("%-20s %-15s %-8s %-5s %s\n", "NAME", "SCHEDULE", "ENABLED", "RUNS", "LAST RUN")
	for _, j := range jobs {
		enabled := "yes"
		if !j.Enabled {
			enabled = "no"
		}
		lastRun := "never"
		if !j.LastRun.IsZero() {
			lastRun = j.LastRun.Format("2006-01-02 15:04:05")
		}
		fmt.Printf("%-20s %-15s %-8s %-5d %s\n", j.Name, j.CronExpr, enabled, j.RunCount, lastRun)
	}
}

func (c *cronCLIClient) add(args []string) {
	if len(args) < 3 {
		log.Fatal("usage: cron add <name> <cron-expr> <input> [--workflow=basic] [--tools=@default] [--system-prompt=TEXT]")
	}
	name := args[0]
	cronExpr := args[1]
	input := args[2]
	wf := "basic"
	var cronTools []string
	systemPrompt := ""
	for _, arg := range args[3:] {
		switch {
		case strings.HasPrefix(arg, "--workflow="):
			wf = strings.TrimPrefix(arg, "--workflow=")
		case strings.HasPrefix(arg, "--tools="):
			cronTools = strings.Split(strings.TrimPrefix(arg, "--tools="), ",")
		case strings.HasPrefix(arg, "--system-prompt="):
			systemPrompt = strings.TrimPrefix(arg, "--system-prompt=")
		}
	}
	body := map[string]any{
		"name":     name,
		"cronExpr": cronExpr,
		"config": map[string]any{
			"input":        input,
			"workflow":     wf,
			"tools":        cronTools,
			"systemPrompt": systemPrompt,
		},
	}
	_, err := c.doRequest(http.MethodPost, "/api/v1/cron/jobs", body)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("✅ Job %q scheduled (%s)\n", name, cronExpr)
}

func (c *cronCLIClient) remove(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: cron remove <name>")
	}
	_, err := c.doRequest(http.MethodDelete, "/api/v1/cron/jobs/"+args[0], nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("✅ Job %q removed\n", args[0])
}

func (c *cronCLIClient) trigger(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: cron trigger <name>")
	}
	data, err := c.doRequest(http.MethodPost, "/api/v1/cron/jobs/"+args[0]+"/trigger", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("✅ Job %q triggered\n", args[0])
	fmt.Println(string(data))
}

func (c *cronCLIClient) setEnabled(args []string, enabled bool) {
	if len(args) < 1 {
		action := "enable"
		if !enabled {
			action = "disable"
		}
		log.Fatalf("usage: cron %s <name>", action)
	}
	_, err := c.doRequest(http.MethodPatch, "/api/v1/cron/jobs/"+args[0], map[string]any{"enabled": enabled})
	if err != nil {
		log.Fatal(err)
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Printf("✅ Job %q %s\n", args[0], state)
}

func (c *cronCLIClient) get(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: cron get <name>")
	}
	data, err := c.doRequest(http.MethodGet, "/api/v1/cron/jobs/"+args[0], nil)
	if err != nil {
		log.Fatal(err)
	}
	var job cronpkg.Job
	if err := json.Unmarshal(data, &job); err != nil {
		log.Fatalf("parse response: %v", err)
	}
	b, _ := json.MarshalIndent(job, "", "  ")
	fmt.Println(string(b))
}

func printCronUsage() {
	fmt.Println("Cron CLI - manage scheduled agent jobs (requires running UI server)")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  go run ./framework cron list                       List all scheduled jobs")
	fmt.Println("  go run ./framework cron add <name> <expr> <input>  Add a new job")
	fmt.Println("  go run ./framework cron remove <name>              Remove a job")
	fmt.Println("  go run ./framework cron trigger <name>             Run a job immediately")
	fmt.Println("  go run ./framework cron enable <name>              Enable a job")
	fmt.Println("  go run ./framework cron disable <name>             Disable a job")
	fmt.Println("  go run ./framework cron get <name>                 Get job details")
	fmt.Println()
	fmt.Println("Global flags:")
	fmt.Println("  --addr=HOST:PORT    UI server address (default: 127.0.0.1:7070)")
	fmt.Println("  --api-key=KEY       API key for authentication")
}
