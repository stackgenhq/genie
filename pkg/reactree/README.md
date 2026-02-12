# pkg/reactree

A thin wrapper around [`trpc-agent-go`](https://github.com/trpc-group/trpc-agent-go)'s `graph.StateGraph` that maps **Behavior Tree (BT) semantics** — Sequence, Fallback, and Parallel control flow — onto state graph primitives.

ReAcTree is inspired by the [ReAcTree paper](https://arxiv.org/abs/2310.03756), which introduces a tree-structured reasoning framework for LLM agents that combines subgoal decomposition with episodic memory.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      TreeExecutor                           │
│  Builds a graph.StateGraph, compiles, runs graph.Executor   │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────┐   ┌──────────────┐   ┌──────────────────┐ │
│  │  node.go     │   │ agent_node.go│   │ control_flow.go  │ │
│  │             │   │              │   │                  │ │
│  │ NodeStatus  │   │ AgentNodeFunc│   │ BuildSequence    │ │
│  │ StateKeys   │   │ (NodeFunc)   │   │ BuildFallback    │ │
│  │ StateSchema │   │              │   │ BuildParallel    │ │
│  └──────┬──────┘   └──────┬───────┘   └────────┬─────────┘ │
│         │                 │                     │           │
│         └────────┬────────┴─────────────────────┘           │
│                  ▼                                           │
│         graph.StateGraph (trpc-agent-go)                    │
│         graph.Executor   (trpc-agent-go)                    │
├─────────────────────────────────────────────────────────────┤
│                      memory/                                │
│  ┌──────────────────┐  ┌─────────────────────────────────┐  │
│  │  working.go       │  │  episodic.go                    │  │
│  │  WorkingMemory    │  │  EpisodicMemory (interface)     │  │
│  │  (KV scratchpad)  │  │  → serviceEpisodicMemory        │  │
│  │                   │  │    (delegates to memory.Service) │  │
│  │                   │  │  → noOpEpisodicMemory            │  │
│  └──────────────────┘  └─────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### Package Layout

```
pkg/reactree/
├── node.go              # NodeStatus enum, StateKeys, NewReAcTreeSchema()
├── agent_node.go        # NewAgentNodeFunc() → graph.NodeFunc wrapping expert.Expert
├── control_flow.go      # BuildSequence / BuildFallback / BuildParallel
├── tree.go              # TreeExecutor interface + default implementation
├── control_flow_test.go # Graph compilation & execution tests
├── init_test.go         # Ginkgo test suite bootstrap
├── memory/
│   ├── working.go       # WorkingMemory — thread-safe KV shared across nodes
│   ├── episodic.go      # EpisodicMemory interface + memory.Service delegate
│   ├── memory_test.go   # Memory tests (working, episodic, service-backed)
│   └── init_test.go     # Memory test suite bootstrap
└── reactreefakes/       # counterfeiter-generated fakes
```

---

## Key Concepts

### Control Flow → Graph Primitive Mapping

| BT Pattern | ReAcTree Builder | Graph Primitive | Semantics |
|---|---|---|---|
| **Sequence** | `BuildSequence(sg, nodeIDs)` | `AddConditionalEdges` | success→next, failure→END (AND) |
| **Fallback** | `BuildFallback(sg, nodeIDs)` | `AddConditionalEdges` | success→END, failure→next (OR) |
| **Parallel** | `BuildParallel(sg, nodeIDs, aggID)` | `AddJoinEdge` + aggregator | Majority vote (fan-out/fan-in) |

### State Graph Schema

All nodes share state via `graph.State` using these keys:

| Key | Type | Purpose |
|---|---|---|
| `reactree_goal` | `string` | The current task goal |
| `reactree_node_status` | `NodeStatus` | Last node's `Success`/`Failure`/`Running` |
| `reactree_output` | `string` | Text output from the last node |
| `reactree_working_memory` | `map[string]any` | Shared observations across nodes |

### Memory System

| Component | Type | Backend | Purpose |
|---|---|---|---|
| **WorkingMemory** | Concrete struct | In-memory KV | Ephemeral scratchpad for a single tree run |
| **EpisodicMemory** | Interface | `memory.Service` | Long-term subgoal experience storage & retrieval |

---

## Usage

### Basic: Single-Goal Execution

```go
import (
    "github.com/appcd-dev/genie/pkg/reactree"
    "github.com/appcd-dev/genie/pkg/reactree/memory"
)

// Create memory instances
wm := memory.NewWorkingMemory()
ep := memory.NewNoOpEpisodicMemory() // or NewServiceEpisodicMemory(cfg)

// Create executor
executor := reactree.NewTreeExecutor(myExpert, wm, ep, reactree.DefaultTreeConfig())

// Run
result, err := executor.Run(ctx, reactree.TreeRequest{
    Goal:      "Analyze the Terraform configuration and suggest improvements",
    EventChan: eventCh,
})
```

### With memory.Service-Backed Episodic Memory

```go
import (
    "github.com/appcd-dev/genie/pkg/reactree/memory"
    "trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
)

svc := inmemory.NewMemoryService()
defer svc.Close()

ep := memory.EpisodicMemoryConfig{
    Service: svc,
    AppName: "my-app",
    UserID:  "user-123",
}.NewServiceEpisodicMemory()

executor := reactree.NewTreeExecutor(myExpert, nil, ep, reactree.DefaultTreeConfig())
```

### Building Custom Graph Topologies

```go
schema := reactree.NewReAcTreeSchema()
sg := graph.NewStateGraph(schema)

// Add agent nodes
sg.AddNode("analyze", reactree.NewAgentNodeFunc(analyzeConfig))
sg.AddNode("fix",     reactree.NewAgentNodeFunc(fixConfig))
sg.AddNode("verify",  reactree.NewAgentNodeFunc(verifyConfig))
sg.SetEntryPoint("analyze")

// Wire as sequence: analyze → fix → verify (fail-fast on any failure)
reactree.BuildSequence(sg, []string{"analyze", "fix", "verify"})

// Compile & execute
compiled, _ := sg.Compile()
executor, _ := graph.NewExecutor(compiled)
events, _   := executor.Execute(ctx, graph.State{
    reactree.StateKeyGoal: "Fix all lint errors",
}, invocation)
```

---

## Configuration

### TreeConfig

```go
reactree.TreeConfig{
    MaxDepth:            3,   // Max tree depth for recursive expansion
    MaxDecisionsPerNode: 10,  // Max LLM calls per agent node
    MaxTotalNodes:       20,  // Max total nodes (maps to graph.WithMaxSteps)
}
```

Use `reactree.DefaultTreeConfig()` for sensible defaults.

---

## Runbook

### Running Tests

```bash
# Run all reactree tests
go test -v -count=1 ./pkg/reactree/...

# Run memory sub-package tests only
go test -v -count=1 ./pkg/reactree/memory/...

# Run with race detector
go test -race -count=1 ./pkg/reactree/...
```

### Regenerating Fakes

```bash
go generate ./pkg/reactree/...
go generate ./pkg/reactree/memory/...
```

This regenerates counterfeiter fakes for:
- `TreeExecutor` → `reactreefakes/fake_tree_executor.go`
- `EpisodicMemory` → `memory/memoryfakes/fake_episodic_memory.go` (after running generate)

### Adding a New Control Flow Pattern

1. Create a `Build<Pattern>(sg, nodeIDs, ...) *graph.StateGraph` function in `control_flow.go`
2. Use `graph.AddConditionalEdges`, `graph.AddEdge`, or `graph.AddJoinEdge` to wire the topology
3. The `statusRouter` function routes based on `StateKeyNodeStatus` — reuse it or create a custom router
4. Add compilation and execution tests in `control_flow_test.go`

### Adding a New Agent Node Type

1. Define a config struct with the required fields
2. Create a factory function returning `graph.NodeFunc`
3. Read inputs from `graph.State`, perform work, return a `graph.State` with at minimum:
   - `StateKeyNodeStatus` → `Success` or `Failure`
   - `StateKeyOutput` → text result
4. Register the node with `sg.AddNode(id, yourFunc)`

### Integrating ReAcTree into a New Expert

Follow the pattern in `pkg/codeowner/expert.go`:

```go
func NewExpert(/* ... */) *Expert {
    wm := memory.NewWorkingMemory()
    treeExec := reactree.NewTreeExecutor(exp, wm, nil, reactree.DefaultTreeConfig())

    return &Expert{
        workingMemory: wm,
        treeExecutor:  treeExec,
    }
}

func (e *Expert) Chat(ctx context.Context, msg string) (string, error) {
    result, err := e.treeExecutor.Run(ctx, reactree.TreeRequest{
        Goal: msg,
    })
    return result.Output, err
}
```

### Debugging

- Set log level to `debug` to see agent node prompts, output lengths, and majority vote results
- Check `StateKeyNodeStatus` in events to trace success/failure paths through the graph
- Use `wm.Snapshot()` to inspect the working memory at any point during execution

---

## Dependencies

| Package | Purpose |
|---|---|
| `trpc-agent-go/graph` | `StateGraph`, `Executor`, `NodeFunc`, `State`, `StateSchema` |
| `trpc-agent-go/memory` | `memory.Service` for episodic memory backend |
| `trpc-agent-go/memory/inmemory` | In-memory `memory.Service` implementation |
| `pkg/expert` | LLM expert interface used by agent nodes |
| `go-lib/logger` | Structured logging |
