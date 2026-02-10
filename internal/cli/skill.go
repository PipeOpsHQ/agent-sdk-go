package cli

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/PipeOpsHQ/agent-sdk-go/skill"
)

func runSkillCLI(args []string) {
	if len(args) == 0 {
		printSkillUsage()
		os.Exit(1)
	}

	skill.RegisterBuiltins()
	skill.ScanDefaults()

	switch args[0] {
	case "list", "ls":
		names := skill.Names()
		if len(names) == 0 {
			fmt.Println("No skills installed.")
			return
		}
		fmt.Printf("%-25s %-10s %s\n", "NAME", "SOURCE", "DESCRIPTION")
		for _, name := range names {
			s, _ := skill.Get(name)
			desc := s.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Printf("%-25s %-10s %s\n", s.Name, s.Source, desc)
		}

	case "show":
		if len(args) < 2 {
			log.Fatal("usage: skill show <name>")
		}
		s, ok := skill.Get(args[1])
		if !ok {
			log.Fatalf("skill %q not found", args[1])
		}
		fmt.Printf("Name:         %s\n", s.Name)
		fmt.Printf("Description:  %s\n", s.Description)
		fmt.Printf("Source:       %s\n", s.Source)
		fmt.Printf("License:      %s\n", s.License)
		fmt.Printf("Path:         %s\n", s.Path)
		if len(s.AllowedTools) > 0 {
			fmt.Printf("AllowedTools: %s\n", strings.Join(s.AllowedTools, ", "))
		}
		for k, v := range s.Metadata {
			fmt.Printf("Metadata.%s: %s\n", k, v)
		}
		if s.Instructions != "" {
			fmt.Println("\n--- Instructions ---")
			fmt.Println(s.Instructions)
		}

	case "install":
		if len(args) < 2 {
			log.Fatal("usage: skill install <github-repo> [--dest=./skills]")
		}
		destDir := "./skills"
		repoRef := args[1]
		for _, a := range args[2:] {
			if strings.HasPrefix(a, "--dest=") {
				destDir = strings.TrimPrefix(a, "--dest=")
			}
		}
		n, err := skill.InstallFromGitHub(repoRef, destDir)
		if err != nil {
			log.Fatalf("install failed: %v", err)
		}
		fmt.Printf("Installed %d skill(s) from %s -> %s\n", n, repoRef, destDir)

	case "remove", "rm":
		if len(args) < 2 {
			log.Fatal("usage: skill remove <name>")
		}
		if skill.Remove(args[1]) {
			fmt.Printf("Removed skill %q\n", args[1])
		} else {
			log.Fatalf("skill %q not found", args[1])
		}

	case "create":
		if len(args) < 3 {
			log.Fatal("usage: skill create <name> <description>")
		}
		name := args[1]
		desc := strings.Join(args[2:], " ")
		destDir := "./skills"
		skillDir := filepath.Join(destDir, name)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			log.Fatalf("failed to create directory: %v", err)
		}
		content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n# %s\n\nAdd your instructions here.\n", name, desc, name)
		path := filepath.Join(skillDir, "SKILL.md")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			log.Fatalf("failed to write SKILL.md: %v", err)
		}
		fmt.Printf("Created skill scaffold: %s\n", path)

	default:
		log.Fatalf("unknown skill command %q", args[0])
	}
}

func printSkillUsage() {
	fmt.Println("Usage: agent skill <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  list                        List installed skills")
	fmt.Println("  show <name>                 Show skill details and instructions")
	fmt.Println("  install <repo> [--dest=DIR] Install skills from GitHub repo")
	fmt.Println("  remove <name>               Remove a skill from registry")
	fmt.Println("  create <name> <description> Create a new skill scaffold")
}
