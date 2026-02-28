package math

import (
	"context"
	"math"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Math Tool (unified arithmetic)", func() {
	var m *mathTools

	BeforeEach(func() {
		m = newMathTools()
	})

	DescribeTable("performs arithmetic correctly",
		func(op string, a, b, expected float64) {
			resp, err := m.doMath(context.Background(), mathRequest{Operation: op, A: a, B: b})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Operation).To(Equal(op))
			Expect(resp.A).To(Equal(a))
			Expect(resp.B).To(Equal(b))
			Expect(resp.Result).To(Equal(expected))
		},
		// add
		Entry("add: positive numbers", "add", 2.0, 3.0, 5.0),
		Entry("add: negative numbers", "add", -2.0, -3.0, -5.0),
		Entry("add: mixed signs", "add", -2.0, 3.0, 1.0),
		Entry("add: zeros", "add", 0.0, 0.0, 0.0),
		Entry("add: decimals", "add", 1.5, 2.5, 4.0),
		Entry("add: large numbers", "add", 1e15, 2e15, 3e15),

		// subtract
		Entry("subtract: positive numbers", "subtract", 5.0, 3.0, 2.0),
		Entry("subtract: result negative", "subtract", 3.0, 5.0, -2.0),
		Entry("subtract: zeros", "subtract", 0.0, 0.0, 0.0),
		Entry("subtract: decimals", "subtract", 5.5, 2.5, 3.0),

		// multiply
		Entry("multiply: positive numbers", "multiply", 3.0, 4.0, 12.0),
		Entry("multiply: by zero", "multiply", 5.0, 0.0, 0.0),
		Entry("multiply: negative result", "multiply", -3.0, 4.0, -12.0),
		Entry("multiply: both negative", "multiply", -3.0, -4.0, 12.0),
		Entry("multiply: decimals", "multiply", 2.5, 4.0, 10.0),

		// divide
		Entry("divide: normal division", "divide", 10.0, 2.0, 5.0),
		Entry("divide: decimal result", "divide", 7.0, 2.0, 3.5),
	)

	It("returns an error for division by zero", func() {
		_, err := m.doMath(context.Background(), mathRequest{Operation: "divide", A: 10, B: 0})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("division by zero"))
	})

	It("returns an error for unsupported operations", func() {
		_, err := m.doMath(context.Background(), mathRequest{Operation: "modulo", A: 10, B: 3})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported operation"))
	})

	It("normalises operation case", func() {
		resp, err := m.doMath(context.Background(), mathRequest{Operation: "ADD", A: 1, B: 2})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Result).To(Equal(3.0))
	})
})

var _ = Describe("Calculator Tool (expression evaluator)", func() {
	var m *mathTools

	BeforeEach(func() {
		m = newMathTools()
	})

	DescribeTable("evaluates valid expressions",
		func(expression string, expected float64) {
			resp, err := m.calculate(context.Background(), expressionRequest{Expression: expression})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Expression).To(Equal(expression))
			Expect(resp.Result).To(BeNumerically("~", expected, 1e-9))
		},
		Entry("simple addition", "2+3", 5.0),
		Entry("simple subtraction", "10-4", 6.0),
		Entry("simple multiplication", "3*4", 12.0),
		Entry("simple division", "10/2", 5.0),
		Entry("operator precedence", "2+3*4", 14.0),
		Entry("parentheses", "(2+3)*4", 20.0),
		Entry("nested parentheses", "((2+3)*4)+1", 21.0),
		Entry("decimal numbers", "1.5+2.5", 4.0),
		Entry("negative unary", "-5+8", 3.0),
		Entry("sqrt function", "sqrt(16)", 4.0),
		Entry("abs of negative", "abs(-5)", 5.0),
		Entry("pow function", "pow(2,10)", 1024.0),
		Entry("log base 10", "log(100)", 2.0),
		Entry("ln of e", "ln(e)", 1.0),
		Entry("pi constant", "pi", math.Pi),
		Entry("complex expression", "sqrt(pow(3,2)+pow(4,2))", 5.0),
	)

	DescribeTable("returns errors for invalid expressions",
		func(expression string) {
			_, err := m.calculate(context.Background(), expressionRequest{Expression: expression})
			Expect(err).To(HaveOccurred())
		},
		Entry("empty expression", ""),
		Entry("division by zero", "1/0"),
		Entry("sqrt of negative", "sqrt(-4)"),
		Entry("ln of zero", "ln(0)"),
		Entry("log of negative", "log(-1)"),
		Entry("unknown function", "foo(5)"),
		Entry("pow missing arg", "pow(2)"),
	)

	It("rejects expressions exceeding the length limit", func() {
		// Build a 1001-character expression.
		longExpr := "1"
		for len(longExpr) < 1001 {
			longExpr += "+1"
		}
		_, err := m.calculate(context.Background(), expressionRequest{Expression: longExpr})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("too long"))
	})
})

var _ = Describe("ToolProvider", func() {
	It("returns the expected set of tools", func() {
		p := NewToolProvider()
		tools := p.GetTools()

		Expect(tools).To(HaveLen(2))

		names := make([]string, len(tools))
		for i, tl := range tools {
			names[i] = tl.Declaration().Name
		}
		Expect(names).To(ConsistOf("math", "calculator"))
	})
})
