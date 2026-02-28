// Package reactree implements the ReAcTree (Reasoning-Acting Tree) execution
// engine for Genie's multi-step agent workflows.
//
// It solves the problem of running complex tasks as a behavior tree: the
// orchestrator builds a graph of nodes (sequence, selector, agent nodes), and
// the tree is ticked until completion. Each agent node runs an Expert (LLM +
// tools); control flow (retry, fallback, parallel stages) is expressed as tree
// structure rather than ad-hoc code. Without this package, multi-step flows
// would be hard-coded and harder to extend or reason about.
package reactree
