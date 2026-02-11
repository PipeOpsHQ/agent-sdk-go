package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type memoryStoreArgs struct {
	Operation string `json:"operation"`
	Key       string `json:"key,omitempty"`
	Value     any    `json:"value,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	TTL       int    `json:"ttl,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
}

// MemoryEntry represents a stored memory entry.
type MemoryEntry struct {
	Key       string    `json:"key"`
	Value     any       `json:"value"`
	Namespace string    `json:"namespace"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	TTL       int       `json:"ttl,omitempty"`
}

// MemoryResult contains the result of a memory operation.
type MemoryResult struct {
	Success bool           `json:"success"`
	Data    map[string]any `json:"data,omitempty"`
	Error   string         `json:"error,omitempty"`
}

var (
	memoryMu    sync.RWMutex
	memoryStore = make(map[string]map[string]*MemoryEntry)
)

func NewMemoryStore() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"set", "get", "delete", "list", "search", "clear"},
				"description": "Operation: set, get, delete, list, search, clear.",
			},
			"key": map[string]any{
				"type":        "string",
				"description": "Key for the memory entry.",
			},
			"value": map[string]any{
				"description": "Value to store (any JSON value).",
			},
			"namespace": map[string]any{
				"type":        "string",
				"description": "Namespace for organizing memories. Defaults to 'default'.",
			},
			"ttl": map[string]any{
				"type":        "integer",
				"description": "Time-to-live in seconds. 0 means no expiration.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Pattern for search (supports * wildcard).",
			},
		},
		"required": []string{"operation"},
	}

	return NewFuncTool(
		"memory_store",
		"Store and retrieve information across agent interactions. Supports namespaces, TTL, and pattern search.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			var in memoryStoreArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid memory_store args: %w", err)
			}

			namespace := in.Namespace
			if namespace == "" {
				namespace = "default"
			}

			switch in.Operation {
			case "set":
				return memSet(namespace, in.Key, in.Value, in.TTL)
			case "get":
				return memGet(namespace, in.Key)
			case "delete":
				return memDelete(namespace, in.Key)
			case "list":
				return memList(namespace)
			case "search":
				return memSearch(namespace, in.Pattern)
			case "clear":
				return memClear(namespace)
			default:
				return nil, fmt.Errorf("unsupported operation %q", in.Operation)
			}
		},
	)
}

func memSet(namespace, key string, value any, ttl int) (*MemoryResult, error) {
	if key == "" {
		return &MemoryResult{Success: false, Error: "key is required"}, nil
	}

	memoryMu.Lock()
	defer memoryMu.Unlock()

	if memoryStore[namespace] == nil {
		memoryStore[namespace] = make(map[string]*MemoryEntry)
	}

	entry := &MemoryEntry{
		Key:       key,
		Value:     value,
		Namespace: namespace,
		CreatedAt: time.Now(),
		TTL:       ttl,
	}

	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(time.Duration(ttl) * time.Second)
	}

	memoryStore[namespace][key] = entry

	return &MemoryResult{
		Success: true,
		Data: map[string]any{
			"key":       key,
			"namespace": namespace,
			"ttl":       ttl,
			"message":   "value stored",
		},
	}, nil
}

func memGet(namespace, key string) (*MemoryResult, error) {
	if key == "" {
		return &MemoryResult{Success: false, Error: "key is required"}, nil
	}

	memoryMu.RLock()
	store := memoryStore[namespace]
	if store == nil {
		memoryMu.RUnlock()
		return &MemoryResult{Success: true, Data: map[string]any{"found": false, "key": key}}, nil
	}
	entry := store[key]
	memoryMu.RUnlock()

	if entry == nil {
		return &MemoryResult{Success: true, Data: map[string]any{"found": false, "key": key}}, nil
	}

	if entry.TTL > 0 && time.Now().After(entry.ExpiresAt) {
		memoryMu.Lock()
		delete(memoryStore[namespace], key)
		memoryMu.Unlock()
		return &MemoryResult{Success: true, Data: map[string]any{"found": false, "key": key, "expired": true}}, nil
	}

	return &MemoryResult{
		Success: true,
		Data: map[string]any{
			"found":     true,
			"key":       key,
			"value":     entry.Value,
			"createdAt": entry.CreatedAt.Format(time.RFC3339),
			"ttl":       entry.TTL,
		},
	}, nil
}

func memDelete(namespace, key string) (*MemoryResult, error) {
	if key == "" {
		return &MemoryResult{Success: false, Error: "key is required"}, nil
	}

	memoryMu.Lock()
	defer memoryMu.Unlock()

	store := memoryStore[namespace]
	existed := false
	if store != nil {
		_, existed = store[key]
		delete(store, key)
	}

	return &MemoryResult{
		Success: true,
		Data:    map[string]any{"key": key, "deleted": existed},
	}, nil
}

func memList(namespace string) (*MemoryResult, error) {
	memoryMu.Lock()
	defer memoryMu.Unlock()

	store := memoryStore[namespace]
	if store == nil {
		return &MemoryResult{
			Success: true,
			Data:    map[string]any{"namespace": namespace, "keys": []string{}, "count": 0},
		}, nil
	}

	now := time.Now()
	keys := make([]string, 0)
	entries := make([]map[string]any, 0)

	for key, entry := range store {
		if entry.TTL > 0 && now.After(entry.ExpiresAt) {
			delete(store, key)
			continue
		}
		keys = append(keys, key)
		entries = append(entries, map[string]any{
			"key":       key,
			"createdAt": entry.CreatedAt.Format(time.RFC3339),
			"ttl":       entry.TTL,
		})
	}

	return &MemoryResult{
		Success: true,
		Data: map[string]any{
			"namespace": namespace,
			"keys":      keys,
			"entries":   entries,
			"count":     len(keys),
		},
	}, nil
}

func memSearch(namespace, pattern string) (*MemoryResult, error) {
	if pattern == "" {
		return &MemoryResult{Success: false, Error: "pattern is required"}, nil
	}

	memoryMu.Lock()
	defer memoryMu.Unlock()

	store := memoryStore[namespace]
	if store == nil {
		return &MemoryResult{
			Success: true,
			Data:    map[string]any{"namespace": namespace, "pattern": pattern, "matches": []any{}, "count": 0},
		}, nil
	}

	now := time.Now()
	matches := make([]map[string]any, 0)

	for key, entry := range store {
		if entry.TTL > 0 && now.After(entry.ExpiresAt) {
			delete(store, key)
			continue
		}

		if matchWildcard(pattern, key) {
			matches = append(matches, map[string]any{
				"key":       key,
				"value":     entry.Value,
				"createdAt": entry.CreatedAt.Format(time.RFC3339),
			})
		}
	}

	return &MemoryResult{
		Success: true,
		Data: map[string]any{
			"namespace": namespace,
			"pattern":   pattern,
			"matches":   matches,
			"count":     len(matches),
		},
	}, nil
}

func memClear(namespace string) (*MemoryResult, error) {
	memoryMu.Lock()
	defer memoryMu.Unlock()

	count := len(memoryStore[namespace])
	delete(memoryStore, namespace)

	return &MemoryResult{
		Success: true,
		Data: map[string]any{
			"namespace": namespace,
			"cleared":   count,
			"message":   fmt.Sprintf("cleared %d entries", count),
		},
	}, nil
}

func matchWildcard(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		return strings.Contains(s, pattern[1:len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(s, pattern[1:])
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(s, pattern[:len(pattern)-1])
	}
	return s == pattern
}

// ClearAllMemory clears all memory stores.
func ClearAllMemory() {
	memoryMu.Lock()
	defer memoryMu.Unlock()
	memoryStore = make(map[string]map[string]*MemoryEntry)
}
