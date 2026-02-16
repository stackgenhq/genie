package reactree

import (
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type ToolRegistry map[string]tool.Tool

func (t ToolRegistry) String() string {
	sb := strings.Builder{}
	for name, tl := range t {
		sb.WriteString("\n")
		sb.WriteString(name)
		sb.WriteString("\t")
		sb.WriteString(tl.Declaration().Description)
	}
	return sb.String()
}

func (t ToolRegistry) Exclude(excludeTools []string) ToolRegistry {
	newRegistry := make(ToolRegistry)
	for k, v := range t {
		newRegistry[k] = v
	}
	for _, excludeTool := range excludeTools {
		delete(newRegistry, excludeTool)
	}
	return newRegistry
}

func (t ToolRegistry) Tools() []tool.Tool {
	var tools []tool.Tool
	for _, tl := range t {
		tools = append(tools, tl)
	}
	return tools
}
