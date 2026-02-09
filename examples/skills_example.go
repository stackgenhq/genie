package main

import (
	"context"
	"fmt"
	"log"

	"github.com/appcd-dev/genie/pkg/config"
	"github.com/appcd-dev/genie/pkg/skills"
	"trpc.group/trpc-go/trpc-agent-go/skill"
)

// This example demonstrates how to use the skills system in Genie.
// It shows how to:
// 1. Load skills from a single root
// 2. Load skills from multiple roots (local + remote HTTPS URLs)
// 3. List available skills
// 4. Load a specific skill
// 5. Execute a skill script

func main() {
	ctx := context.Background()

	// Example 1: Load skills from single root
	cfg := config.GenieConfig{
		SkillsRoots: []string{"./examples/skills"},
	}

	tools, err := skills.LoadSkillsFromConfig(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to load skills: %v", err)
	}

	fmt.Printf("Loaded %d skill tools from single root\n", len(tools))
	for _, tool := range tools {
		decl := tool.Declaration()
		fmt.Printf("- %s: %s\n", decl.Name, decl.Description)
	}

	// Example 2: Load skills from multiple roots (local + remote)
	cfgMulti := config.GenieConfig{
		SkillsRoots: []string{
			"./examples/skills",
			// Example remote skills repository (uncomment to test with actual URL)
			// "https://raw.githubusercontent.com/your-org/skills-repo/main/skills",
		},
	}

	toolsMulti, err := skills.LoadSkillsFromConfig(ctx, cfgMulti)
	if err != nil {
		log.Fatalf("Failed to load skills from multiple roots: %v", err)
	}

	fmt.Printf("\nLoaded %d skill tools from multiple roots\n", len(toolsMulti))
	for _, tool := range toolsMulti {
		decl := tool.Declaration()
		fmt.Printf("- %s: %s\n", decl.Name, decl.Description)
	}

	// Example: Create skills repository directly
	repo, err := skill.NewFSRepository("./examples/skills")
	if err != nil {
		log.Fatalf("Failed to create repository: %v", err)
	}

	// List all available skills
	summaries := repo.Summaries()
	fmt.Printf("\nAvailable skills (%d):\n", len(summaries))
	for _, s := range summaries {
		fmt.Printf("- %s: %s\n", s.Name, s.Description)
	}

	// Load a specific skill
	if len(summaries) > 0 {
		skillName := summaries[0].Name
		skill, err := repo.Get(skillName)
		if err != nil {
			log.Fatalf("Failed to get skill: %v", err)
		}

		fmt.Printf("\nSkill: %s\n", skill.Summary.Name)
		fmt.Printf("Description: %s\n", skill.Summary.Description)
		fmt.Printf("Instructions:\n%s\n", skill.Body)

		// Get skill path for execution
		skillPath, err := repo.Path(skillName)
		if err != nil {
			log.Fatalf("Failed to get skill path: %v", err)
		}
		fmt.Printf("Skill path: %s\n", skillPath)
	}
}
