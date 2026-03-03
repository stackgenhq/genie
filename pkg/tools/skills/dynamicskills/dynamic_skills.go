package dynamicskills

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// Skill defines a bundled set of tools for a specific domain.
type Skill struct {
	Name        string
	Description string
	Tools       []tool.Tool
}

// SkillRegistry provides access to the available skills.
type SkillRegistry interface {
	Search(query string) []Skill
	Get(name string) (Skill, bool)
}

const defaultMaxLoadedSkills = 3

// DynamicSkillLoader manages the currently loaded skills for an agent loop.
type DynamicSkillLoader struct {
	mu           sync.RWMutex
	registry     SkillRegistry
	loadedSkills map[string]Skill
	maxSkills    int
}

func NewDynamicSkillLoader(registry SkillRegistry, maxSkills int) *DynamicSkillLoader {
	if maxSkills <= 0 {
		maxSkills = defaultMaxLoadedSkills
	}
	return &DynamicSkillLoader{
		registry:     registry,
		loadedSkills: make(map[string]Skill),
		maxSkills:    maxSkills,
	}
}

// GetLoadedTools returns all tools from currently loaded skills.
func (dsl *DynamicSkillLoader) GetLoadedTools() []tool.Tool {
	dsl.mu.RLock()
	defer dsl.mu.RUnlock()

	var tools []tool.Tool
	for _, skill := range dsl.loadedSkills {
		tools = append(tools, skill.Tools...)
	}
	return tools
}

// Registry returns the underlying registry.
func (dsl *DynamicSkillLoader) Registry() SkillRegistry {
	return dsl.registry
}

// MaxSkills returns the maximum number of skills that can be loaded simultaneously.
func (dsl *DynamicSkillLoader) MaxSkills() int {
	return dsl.maxSkills
}

// LoadSkill checks limits and loads a skill.
func (dsl *DynamicSkillLoader) LoadSkill(name string) error {
	dsl.mu.Lock()
	defer dsl.mu.Unlock()

	if _, ok := dsl.loadedSkills[name]; ok {
		return fmt.Errorf("skill %q is already loaded", name)
	}

	if len(dsl.loadedSkills) >= dsl.maxSkills {
		return fmt.Errorf("cannot load %s, max active skills (%d) reached. You must unload a skill first", name, dsl.maxSkills)
	}

	skill, ok := dsl.registry.Get(name)
	if !ok {
		return fmt.Errorf("skill %q not found in registry", name)
	}

	dsl.loadedSkills[name] = skill
	return nil
}

// UnloadSkill unloads a previously loaded skill.
func (dsl *DynamicSkillLoader) UnloadSkill(name string) error {
	dsl.mu.Lock()
	defer dsl.mu.Unlock()

	if _, ok := dsl.loadedSkills[name]; !ok {
		return fmt.Errorf("skill %q is not currently loaded", name)
	}

	delete(dsl.loadedSkills, name)
	return nil
}

func (dsl *DynamicSkillLoader) loadedSkillNames() []string {
	dsl.mu.RLock()
	defer dsl.mu.RUnlock()
	var names []string
	for name := range dsl.loadedSkills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsLoaded checks if a skill is currently loaded.
func (dsl *DynamicSkillLoader) IsLoaded(name string) bool {
	dsl.mu.RLock()
	defer dsl.mu.RUnlock()
	_, ok := dsl.loadedSkills[name]
	return ok
}

// discoverSkillsReq is the input schema for discover_skills.
type discoverSkillsReq struct {
	Query string `json:"query" jsonschema:"description=Search term to find relevant skills. Leave empty to list all."`
}

func DiscoverSkillsTool(registry SkillRegistry) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req discoverSkillsReq) (string, error) {
			skills := registry.Search(req.Query)
			if len(skills) == 0 {
				return "No skills found matching the query.", nil
			}

			var sb strings.Builder
			sb.WriteString("Available skills:\n")
			for _, s := range skills {
				fmt.Fprintf(&sb, "- %s: %s\n", s.Name, s.Description)
			}
			return sb.String(), nil
		},
		function.WithName("discover_skills"),
		function.WithDescription("Search the skill catalog for available specialized tools. Returns a list of skill names and descriptions."),
	)
}

// loadSkillReq is the input schema for load_skill.
type loadSkillReq struct {
	Name string `json:"name" jsonschema:"description=Name of the skill to load (exact match from discover_skills).,required"`
}

func LoadSkillTool(loader *DynamicSkillLoader) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req loadSkillReq) (string, error) {
			logr := logger.GetLogger(ctx).With("fn", "loadSkillTool", "skill", req.Name)
			err := loader.LoadSkill(req.Name)
			if err != nil {
				logr.Warn("failed to load skill", "error", err)
				return "", err
			}

			logr.Info("successfully loaded skill", "currently_loaded", loader.loadedSkillNames())
			return fmt.Sprintf("Successfully loaded skill %q. Its tools will be available in your next action.", req.Name), nil
		},
		function.WithName("load_skill"),
		function.WithDescription(fmt.Sprintf("Load a skill to gain access to its specialized tools. You can load a maximum of %d skills at a time. Loading a skill makes its tools available in your next turn.", loader.maxSkills)),
	)
}

// unloadSkillReq is the input schema for unload_skill.
type unloadSkillReq struct {
	Name string `json:"name" jsonschema:"description=Name of the skill to unload.,required"`
}

func UnloadSkillTool(loader *DynamicSkillLoader) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, req unloadSkillReq) (string, error) {
			err := loader.UnloadSkill(req.Name)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Successfully unloaded skill %q to free up capacity.", req.Name), nil
		},
		function.WithName("unload_skill"),
		function.WithDescription("Unload a currently active skill to free up your active skill capacity budget."),
	)
}
