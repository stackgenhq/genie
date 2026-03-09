package reactree

import "trpc.group/trpc-go/trpc-agent-go/tool"

// testToolProvider satisfies tools.ToolProviders for tests.
type testToolProvider struct{ t []tool.Tool }

func (p *testToolProvider) GetTools() []tool.Tool { return p.t }
