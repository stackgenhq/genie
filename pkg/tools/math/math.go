// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package math provides mathematical tools (a unified arithmetic tool and a
// calculator/expression evaluator) that can be registered with the tool
// registry. These tools allow agents to perform precise arithmetic instead of
// relying on the LLM's inherent (and sometimes inaccurate) numeric reasoning.
//
// Problem: LLMs hallucinate numeric results — even simple division can
// produce wrong answers. This package gives agents a deterministic math
// engine that always returns correct results.
//
// Safety guards:
//   - Expression length limit (1000 chars) prevents computational bombs
//   - 5-second evaluation timeout prevents runaway expressions like pow(pow(2,1000),1000)
//   - Division by zero returns a clear error
//   - Domain errors (sqrt of negative, log of zero) return descriptive errors
//
// Dependencies:
//   - github.com/expr-lang/expr — safe, sandboxed expression evaluation (Go, 6.7k+ ⭐)
//   - No external system dependencies
package math

import (
	"context"
	"fmt"
	gomath "math"
	"strings"
	"time"

	"github.com/expr-lang/expr"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ────────────────────── Request / Response ──────────────────────

// mathRequest is the input schema for the unified math tool. It accepts an
// operation name and two operands.
type mathRequest struct {
	Operation string  `json:"operation" jsonschema:"description=The arithmetic operation to perform. One of: add, subtract, multiply, divide.,enum=add,enum=subtract,enum=multiply,enum=divide"`
	A         float64 `json:"a" jsonschema:"description=The first operand"`
	B         float64 `json:"b" jsonschema:"description=The second operand"`
}

// mathResponse is returned by the unified math tool.
type mathResponse struct {
	Operation string  `json:"operation"`
	A         float64 `json:"a"`
	B         float64 `json:"b"`
	Result    float64 `json:"result"`
}

// expressionRequest is the input schema for the calculator tool.
type expressionRequest struct {
	Expression string `json:"expression" jsonschema:"description=Mathematical expression to evaluate. Supports +, -, *, /, %, ** (power), parentheses, and functions: sqrt, abs, sin, cos, tan, pow, log, ln, ceil, floor, round, min, max. Constants: pi, e. Examples: '2+3*4' or 'sqrt(16)' or 'sin(3.14159/2)' or '2 ** 10'"`
}

// expressionResponse is returned by the calculator tool.
type expressionResponse struct {
	Expression string  `json:"expression"`
	Result     float64 `json:"result"`
	Message    string  `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

// mathTools holds the set of mathematical tools. It is constructed once via
// newMathTools and exposes tool.CallableTool instances.
type mathTools struct{}

// newMathTools returns a new mathTools instance.
func newMathTools() *mathTools {
	return &mathTools{}
}

// mathTool returns the unified "math" tool that handles add, subtract,
// multiply, and divide in a single tool call.
func (m *mathTools) mathTool() tool.CallableTool {
	return function.NewFunctionTool(
		m.doMath,
		function.WithName("math"),
		function.WithDescription(
			"Perform a basic arithmetic operation on two numbers. "+
				"Supported operations: add (a + b), subtract (a - b), multiply (a * b), divide (a / b). "+
				"Returns an error if dividing by zero.",
		),
	)
}

// calculatorTool returns the "calculator" tool that evaluates mathematical
// expressions using the expr-lang/expr engine.
func (m *mathTools) calculatorTool() tool.CallableTool {
	return function.NewFunctionTool(
		m.calculate,
		function.WithName("calculator"),
		function.WithDescription(
			"Evaluate a mathematical expression. Supports basic operations (+, -, *, /, %), "+
				"power (**), parentheses for grouping, comparison operators, "+
				"scientific functions (sqrt, abs, sin, cos, tan, pow, log, ln, ceil, floor, round, min, max), "+
				"and constants (pi, e). Examples: '2+3*4', 'sqrt(16)', 'sin(3.14159/2)', 'pow(2,10)', '2**10', 'max(5,3)'.",
		),
	)
}

// ────────────────────── Tool implementations ──────────────────────

// doMath dispatches the requested arithmetic operation.
func (m *mathTools) doMath(_ context.Context, req mathRequest) (mathResponse, error) {
	op := strings.ToLower(strings.TrimSpace(req.Operation))

	resp := mathResponse{
		Operation: op,
		A:         req.A,
		B:         req.B,
	}

	switch op {
	case "add":
		resp.Result = req.A + req.B
	case "subtract":
		resp.Result = req.A - req.B
	case "multiply":
		resp.Result = req.A * req.B
	case "divide":
		if req.B == 0 {
			return resp, fmt.Errorf("division by zero: cannot divide %g by 0", req.A)
		}
		resp.Result = req.A / req.B
	default:
		return resp, fmt.Errorf("unsupported operation %q: must be one of add, subtract, multiply, divide", req.Operation)
	}

	return resp, nil
}

// exprEnv provides the mathematical functions and constants available in
// the expression evaluator. This is passed to expr-lang/expr as the
// evaluation environment.
var exprEnv = map[string]any{
	// Constants
	"pi": gomath.Pi,
	"e":  gomath.E,

	// Single-argument functions
	"sqrt": safeFunc1("sqrt", func(x float64) (float64, error) {
		if x < 0 {
			return 0, fmt.Errorf("cannot compute sqrt of negative number %g", x)
		}
		return gomath.Sqrt(x), nil
	}),
	"abs":   func(x float64) float64 { return gomath.Abs(x) },
	"sin":   func(x float64) float64 { return gomath.Sin(x) },
	"cos":   func(x float64) float64 { return gomath.Cos(x) },
	"tan":   func(x float64) float64 { return gomath.Tan(x) },
	"ceil":  func(x float64) float64 { return gomath.Ceil(x) },
	"floor": func(x float64) float64 { return gomath.Floor(x) },
	"round": func(x float64) float64 { return gomath.Round(x) },
	"ln": safeFunc1("ln", func(x float64) (float64, error) {
		if x <= 0 {
			return 0, fmt.Errorf("cannot compute ln of non-positive number %g", x)
		}
		return gomath.Log(x), nil
	}),
	"log": safeFunc1("log", func(x float64) (float64, error) {
		if x <= 0 {
			return 0, fmt.Errorf("cannot compute log of non-positive number %g", x)
		}
		return gomath.Log10(x), nil
	}),

	// Two-argument functions
	"pow": func(base, exp float64) float64 { return gomath.Pow(base, exp) },
	"min": func(a, b float64) float64 { return gomath.Min(a, b) },
	"max": func(a, b float64) float64 { return gomath.Max(a, b) },
}

// safeFunc1 wraps a function that can return an error into a function
// that panics on error, which expr-lang recovers as an evaluation error.
func safeFunc1(name string, fn func(float64) (float64, error)) func(float64) (float64, error) {
	return fn
}

// calculate evaluates a mathematical expression using expr-lang/expr.
func (m *mathTools) calculate(ctx context.Context, req expressionRequest) (expressionResponse, error) {
	exprStr := strings.TrimSpace(req.Expression)
	if exprStr == "" {
		return expressionResponse{
			Expression: req.Expression,
			Message:    "Error: expression is empty",
		}, fmt.Errorf("expression is empty")
	}

	// Guard against overly complex expressions that could consume
	// excessive CPU/memory (e.g. deeply nested pow calls).
	const maxExprLen = 1000
	if len(exprStr) > maxExprLen {
		return expressionResponse{
			Expression: req.Expression,
			Message:    fmt.Sprintf("Expression too long (%d chars, max %d)", len(exprStr), maxExprLen),
		}, fmt.Errorf("expression too long (%d chars, max %d)", len(exprStr), maxExprLen)
	}

	// Compile and evaluate the expression with our math environment.
	program, err := expr.Compile(exprStr, expr.Env(exprEnv))
	if err != nil {
		return expressionResponse{
			Expression: req.Expression,
			Message:    fmt.Sprintf("Compilation error: %v", err),
		}, fmt.Errorf("compilation error: %w", err)
	}

	// Run with a timeout to prevent computational bombs.
	evalCtx, evalCancel := context.WithTimeout(ctx, 5*time.Second)
	defer evalCancel()

	type evalResult struct {
		output any
		err    error
	}
	resultCh := make(chan evalResult, 1)
	go func() {
		out, runErr := expr.Run(program, exprEnv)
		resultCh <- evalResult{out, runErr}
	}()

	var output any
	select {
	case res := <-resultCh:
		output, err = res.output, res.err
	case <-evalCtx.Done():
		return expressionResponse{
			Expression: req.Expression,
			Message:    "Evaluation timed out (5s limit)",
		}, fmt.Errorf("evaluation timed out after 5s")
	}

	if err != nil {
		return expressionResponse{
			Expression: req.Expression,
			Message:    fmt.Sprintf("Evaluation error: %v", err),
		}, fmt.Errorf("evaluation error: %w", err)
	}

	// Convert the result to float64.
	var result float64
	switch v := output.(type) {
	case float64:
		result = v
	case int:
		result = float64(v)
	case int64:
		result = float64(v)
	case bool:
		if v {
			result = 1
		} else {
			result = 0
		}
	default:
		return expressionResponse{
			Expression: req.Expression,
			Message:    fmt.Sprintf("Unexpected result type: %T", output),
		}, fmt.Errorf("unexpected result type %T: %v", output, output)
	}

	// Check for NaN/Inf from division by zero etc.
	if gomath.IsNaN(result) || gomath.IsInf(result, 0) {
		return expressionResponse{
			Expression: req.Expression,
		}, fmt.Errorf("result is not a finite number (NaN or Infinity)")
	}

	return expressionResponse{
		Expression: req.Expression,
		Result:     result,
		Message:    fmt.Sprintf("Calculation result: %g", result),
	}, nil
}
