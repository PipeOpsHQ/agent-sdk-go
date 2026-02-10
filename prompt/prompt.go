package prompt

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type Spec struct {
	Name        string         `json:"name"`
	Version     string         `json:"version,omitempty"`
	Description string         `json:"description,omitempty"`
	System      string         `json:"system"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
}

type Registry struct {
	mu    sync.RWMutex
	items map[string]map[string]Spec
}

func NewRegistry() *Registry {
	return &Registry{items: map[string]map[string]Spec{}}
}

var global = NewRegistry()

func Register(spec Spec) error { return global.Register(spec) }
func MustRegister(spec Spec) {
	if err := Register(spec); err != nil {
		panic(err)
	}
}
func Resolve(ref string) (Spec, bool) { return global.Resolve(ref) }
func Names() []string                 { return global.Names() }
func List() []Spec                    { return global.List() }
func Delete(ref string) bool          { return global.Delete(ref) }

func (r *Registry) Register(spec Spec) error {
	normalized, err := NormalizeSpec(spec)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[normalized.Name]; !ok {
		r.items[normalized.Name] = map[string]Spec{}
	}
	r.items[normalized.Name][normalized.Version] = normalized
	return nil
}

func (r *Registry) Resolve(ref string) (Spec, bool) {
	name, version := parseRef(ref)
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions, ok := r.items[name]
	if !ok || len(versions) == 0 {
		return Spec{}, false
	}
	if version != "" {
		s, ok := versions[version]
		return s, ok
	}
	keys := make([]string, 0, len(versions))
	for v := range versions {
		keys = append(keys, v)
	}
	sort.Strings(keys)
	return versions[keys[len(keys)-1]], true
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.items))
	for name := range r.items {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) List() []Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []Spec{}
	for _, versions := range r.items {
		for _, spec := range versions {
			out = append(out, spec)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Version < out[j].Version
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (r *Registry) Delete(ref string) bool {
	name, version := parseRef(ref)
	if name == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	versions, ok := r.items[name]
	if !ok {
		return false
	}
	if version == "" {
		delete(r.items, name)
		return true
	}
	if _, ok := versions[version]; !ok {
		return false
	}
	delete(versions, version)
	if len(versions) == 0 {
		delete(r.items, name)
	}
	return true
}

func NormalizeSpec(spec Spec) (Spec, error) {
	spec.Name = strings.ToLower(strings.TrimSpace(spec.Name))
	spec.Version = strings.ToLower(strings.TrimSpace(spec.Version))
	spec.Description = strings.TrimSpace(spec.Description)
	spec.System = strings.TrimSpace(spec.System)
	if spec.Version == "" {
		spec.Version = "v1"
	}
	if spec.Name == "" {
		return Spec{}, fmt.Errorf("prompt name is required")
	}
	if spec.System == "" {
		return Spec{}, fmt.Errorf("prompt %q has empty system text", spec.Name)
	}
	if !isIdentifier(spec.Name) {
		return Spec{}, fmt.Errorf("prompt name %q must match [a-z0-9._-]", spec.Name)
	}
	if !isIdentifier(spec.Version) {
		return Spec{}, fmt.Errorf("prompt version %q must match [a-z0-9._-]", spec.Version)
	}
	return spec, nil
}

func parseRef(ref string) (name string, version string) {
	ref = strings.TrimSpace(strings.ToLower(ref))
	if ref == "" {
		return "", ""
	}
	parts := strings.SplitN(ref, "@", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(parts[0]), ""
}

var identPattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

func isIdentifier(v string) bool {
	return identPattern.MatchString(v)
}
