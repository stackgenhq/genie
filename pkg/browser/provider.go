package browser

import "trpc.group/trpc-go/trpc-agent-go/tool"

// GetTools satisfies the tools.ToolProviders interface so a Browser
// instance can be passed directly to tools.NewRegistry. Without this,
// browser tool construction would be inlined in the registry.
func (b *Browser) GetTools() []tool.Tool {
	callables := AllTools(b)
	out := make([]tool.Tool, len(callables))
	for i, t := range callables {
		out[i] = t
	}
	return out
}
