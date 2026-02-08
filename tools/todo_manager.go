package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type todoArgs struct {
	Operation   string `json:"operation"`
	ID          string `json:"id,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	Priority    string `json:"priority,omitempty"`
	DependsOn   string `json:"dependsOn,omitempty"`
	Tag         string `json:"tag,omitempty"`
}

// TodoItem represents a task in the todo list.
type TodoItem struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	DependsOn   []string  `json:"dependsOn,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

var (
	todoMu    sync.RWMutex
	todoStore = make(map[string]*TodoItem)
)

// NewTodoManager creates the todo_manager tool.
func NewTodoManager() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"add", "get", "update", "remove", "list", "clear"},
				"description": "Operation to perform on the todo list.",
			},
			"id": map[string]any{
				"type":        "string",
				"description": "Todo item ID (kebab-case recommended, e.g. 'fix-auth-bug').",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Short title for the todo item.",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Detailed description of what needs to be done.",
			},
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"pending", "in_progress", "done", "blocked"},
				"description": "Status of the todo item. Default: pending.",
			},
			"priority": map[string]any{
				"type":        "string",
				"enum":        []string{"low", "medium", "high", "critical"},
				"description": "Priority level. Default: medium.",
			},
			"dependsOn": map[string]any{
				"type":        "string",
				"description": "Comma-separated IDs of todos this item depends on.",
			},
			"tag": map[string]any{
				"type":        "string",
				"description": "Tag to add (for add/update) or filter by (for list).",
			},
		},
		"required": []string{"operation"},
	}

	return NewFuncTool(
		"todo_manager",
		"Manage a task/todo list: add, update, remove, and list items with status, priority, dependencies, and tags.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in todoArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid todo_manager args: %w", err)
			}

			switch in.Operation {
			case "add":
				return todoAdd(in)
			case "get":
				return todoGet(in.ID)
			case "update":
				return todoUpdate(in)
			case "remove":
				return todoRemove(in.ID)
			case "list":
				return todoList(in.Status, in.Tag)
			case "clear":
				return todoClear(in.Status)
			default:
				return nil, fmt.Errorf("unsupported operation %q", in.Operation)
			}
		},
	)
}

func todoAdd(in todoArgs) (map[string]any, error) {
	if in.ID == "" {
		return nil, fmt.Errorf("id is required for add")
	}
	if in.Title == "" {
		return nil, fmt.Errorf("title is required for add")
	}

	status := in.Status
	if status == "" {
		status = "pending"
	}
	priority := in.Priority
	if priority == "" {
		priority = "medium"
	}

	now := time.Now()
	item := &TodoItem{
		ID:          in.ID,
		Title:       in.Title,
		Description: in.Description,
		Status:      status,
		Priority:    priority,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if in.DependsOn != "" {
		for _, dep := range strings.Split(in.DependsOn, ",") {
			dep = strings.TrimSpace(dep)
			if dep != "" {
				item.DependsOn = append(item.DependsOn, dep)
			}
		}
	}
	if in.Tag != "" {
		item.Tags = append(item.Tags, in.Tag)
	}

	todoMu.Lock()
	defer todoMu.Unlock()

	if _, exists := todoStore[in.ID]; exists {
		return nil, fmt.Errorf("todo %q already exists", in.ID)
	}
	todoStore[in.ID] = item

	return map[string]any{
		"success": true,
		"message": fmt.Sprintf("todo %q added", in.ID),
		"item":    item,
	}, nil
}

func todoGet(id string) (map[string]any, error) {
	if id == "" {
		return nil, fmt.Errorf("id is required for get")
	}

	todoMu.RLock()
	item, ok := todoStore[id]
	todoMu.RUnlock()

	if !ok {
		return map[string]any{"found": false, "id": id}, nil
	}

	// Check if dependencies are met
	blockedBy := todoBlockedBy(item)

	return map[string]any{
		"found":     true,
		"item":      item,
		"blockedBy": blockedBy,
		"ready":     len(blockedBy) == 0,
	}, nil
}

func todoUpdate(in todoArgs) (map[string]any, error) {
	if in.ID == "" {
		return nil, fmt.Errorf("id is required for update")
	}

	todoMu.Lock()
	defer todoMu.Unlock()

	item, ok := todoStore[in.ID]
	if !ok {
		return nil, fmt.Errorf("todo %q not found", in.ID)
	}

	if in.Title != "" {
		item.Title = in.Title
	}
	if in.Description != "" {
		item.Description = in.Description
	}
	if in.Status != "" {
		item.Status = in.Status
	}
	if in.Priority != "" {
		item.Priority = in.Priority
	}
	if in.DependsOn != "" {
		item.DependsOn = nil
		for _, dep := range strings.Split(in.DependsOn, ",") {
			dep = strings.TrimSpace(dep)
			if dep != "" {
				item.DependsOn = append(item.DependsOn, dep)
			}
		}
	}
	if in.Tag != "" {
		// Add tag if not already present
		found := false
		for _, t := range item.Tags {
			if t == in.Tag {
				found = true
				break
			}
		}
		if !found {
			item.Tags = append(item.Tags, in.Tag)
		}
	}
	item.UpdatedAt = time.Now()

	return map[string]any{
		"success": true,
		"message": fmt.Sprintf("todo %q updated", in.ID),
		"item":    item,
	}, nil
}

func todoRemove(id string) (map[string]any, error) {
	if id == "" {
		return nil, fmt.Errorf("id is required for remove")
	}

	todoMu.Lock()
	defer todoMu.Unlock()

	if _, ok := todoStore[id]; !ok {
		return nil, fmt.Errorf("todo %q not found", id)
	}
	delete(todoStore, id)

	return map[string]any{
		"success": true,
		"message": fmt.Sprintf("todo %q removed", id),
	}, nil
}

func todoList(filterStatus, filterTag string) (map[string]any, error) {
	todoMu.RLock()
	defer todoMu.RUnlock()

	items := make([]*TodoItem, 0, len(todoStore))
	for _, item := range todoStore {
		if filterStatus != "" && item.Status != filterStatus {
			continue
		}
		if filterTag != "" {
			hasTag := false
			for _, t := range item.Tags {
				if t == filterTag {
					hasTag = true
					break
				}
			}
			if !hasTag {
				continue
			}
		}
		items = append(items, item)
	}

	// Sort by priority (critical > high > medium > low), then by creation time
	priorityOrder := map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}
	sort.Slice(items, func(i, j int) bool {
		pi := priorityOrder[items[i].Priority]
		pj := priorityOrder[items[j].Priority]
		if pi != pj {
			return pi < pj
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})

	// Summary counts
	counts := map[string]int{}
	for _, item := range todoStore {
		counts[item.Status]++
	}

	return map[string]any{
		"items":   items,
		"count":   len(items),
		"total":   len(todoStore),
		"summary": counts,
	}, nil
}

func todoClear(status string) (map[string]any, error) {
	todoMu.Lock()
	defer todoMu.Unlock()

	if status == "" {
		count := len(todoStore)
		todoStore = make(map[string]*TodoItem)
		return map[string]any{
			"success": true,
			"cleared": count,
			"message": fmt.Sprintf("cleared all %d todos", count),
		}, nil
	}

	cleared := 0
	for id, item := range todoStore {
		if item.Status == status {
			delete(todoStore, id)
			cleared++
		}
	}

	return map[string]any{
		"success": true,
		"cleared": cleared,
		"message": fmt.Sprintf("cleared %d %s todos", cleared, status),
	}, nil
}

// todoBlockedBy returns IDs of unfinished dependencies.
func todoBlockedBy(item *TodoItem) []string {
	var blocked []string
	for _, depID := range item.DependsOn {
		dep, ok := todoStore[depID]
		if !ok {
			continue
		}
		if dep.Status != "done" {
			blocked = append(blocked, depID)
		}
	}
	return blocked
}

// ClearAllTodos resets the todo store (for testing).
func ClearAllTodos() {
	todoMu.Lock()
	defer todoMu.Unlock()
	todoStore = make(map[string]*TodoItem)
}
