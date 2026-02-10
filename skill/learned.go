package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LearnedPattern represents a behavior the agent discovered and saved.
type LearnedPattern struct {
	Pattern   string    `json:"pattern"`
	Source    string    `json:"source"` // where it was learned from
	CreatedAt time.Time `json:"createdAt"`
}

// CreateSkillFromPatterns generates a SKILL.md file from learned patterns.
func CreateSkillFromPatterns(name, description string, patterns []LearnedPattern, destDir string) (*Skill, error) {
	if name == "" || description == "" {
		return nil, fmt.Errorf("name and description are required")
	}
	if len(patterns) == 0 {
		return nil, fmt.Errorf("at least one pattern is required")
	}

	var instructions strings.Builder
	instructions.WriteString("# " + name + "\n\n")
	instructions.WriteString("## Learned Patterns\n\n")
	instructions.WriteString("The following behaviors were discovered during agent operation:\n\n")

	for i, p := range patterns {
		instructions.WriteString(fmt.Sprintf("### Pattern %d\n", i+1))
		if p.Source != "" {
			instructions.WriteString(fmt.Sprintf("*Source: %s*\n\n", p.Source))
		}
		instructions.WriteString(p.Pattern + "\n\n")
	}

	content := fmt.Sprintf("---\nname: %s\ndescription: %q\nmetadata:\n  origin: learned\n  created: %q\n---\n%s",
		name, description, time.Now().UTC().Format(time.RFC3339), instructions.String())

	// Save to disk
	skillDir := filepath.Join(destDir, name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create skill directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write SKILL.md: %w", err)
	}

	s, err := Parse(content)
	if err != nil {
		return nil, err
	}
	s.Path = skillDir
	s.Source = "learned"
	return s, nil
}
