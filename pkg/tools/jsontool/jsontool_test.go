package jsontool

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("JSON Tool (util_json) — gjson/sjson backed", func() {
	var j *jsonTools

	BeforeEach(func() {
		j = newJSONTools()
	})

	Describe("validate", func() {
		DescribeTable("validates JSON correctly",
			func(input string, expectedValid bool) {
				resp, err := j.jsonOp(context.Background(), jsonRequest{Operation: "validate", JSON: input})
				Expect(err).NotTo(HaveOccurred())
				Expect(*resp.Valid).To(Equal(expectedValid))
			},
			Entry("valid object", `{"key": "value"}`, true),
			Entry("valid array", `[1, 2, 3]`, true),
			Entry("valid primitive", `"hello"`, true),
			Entry("invalid JSON", `{key: value}`, false),
			Entry("incomplete JSON", `{"key":`, false),
		)
	})

	Describe("query (gjson)", func() {
		It("extracts a nested string value", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "query",
				JSON:      `{"data": {"user": {"name": "Alice"}}}`,
				Path:      "data.user.name",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("Alice"))
			Expect(resp.Type).To(Equal("String"))
		})

		It("extracts an array element by index", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "query",
				JSON:      `{"items": ["a", "b", "c"]}`,
				Path:      "items.1",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("b"))
		})

		It("counts array length with #", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "query",
				JSON:      `{"items": [1, 2, 3, 4, 5]}`,
				Path:      "items.#",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("5"))
		})

		It("extracts all values from an array with #.field", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "query",
				JSON:      `{"users": [{"name": "Alice"}, {"name": "Bob"}]}`,
				Path:      "users.#.name",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(ContainSubstring("Alice"))
			Expect(resp.Result).To(ContainSubstring("Bob"))
		})

		It("filters array elements with conditions", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "query",
				JSON:      `{"users": [{"name": "Alice", "age": 30}, {"name": "Bob", "age": 25}]}`,
				Path:      `users.#(age>28).name`,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("Alice"))
		})

		It("extracts a numeric value", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "query",
				JSON:      `{"count": 42}`,
				Path:      "count",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("42"))
		})

		It("extracts a boolean value", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "query",
				JSON:      `{"active": true}`,
				Path:      "active",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(Equal("true"))
		})

		It("returns error for missing path", func() {
			_, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "query",
				JSON:      `{"key": "value"}`,
			})
			Expect(err).To(HaveOccurred())
		})

		It("returns error for non-existent key", func() {
			_, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "query",
				JSON:      `{"key": "value"}`,
				Path:      "missing",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Describe("set (sjson)", func() {
		It("sets a new value at a path", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "set",
				JSON:      `{"name": "Alice"}`,
				Path:      "age",
				Value:     "30",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(ContainSubstring(`"age":30`))
		})

		It("sets a nested value", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "set",
				JSON:      `{"user": {}}`,
				Path:      "user.name",
				Value:     `"Bob"`,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(ContainSubstring(`"name":"Bob"`))
		})

		It("sets an object value", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "set",
				JSON:      `{}`,
				Path:      "config",
				Value:     `{"debug": true}`,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(ContainSubstring(`"debug":true`))
		})

		It("overwrites existing values", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "set",
				JSON:      `{"name": "Alice"}`,
				Path:      "name",
				Value:     `"Bob"`,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(ContainSubstring(`"Bob"`))
		})

		It("returns error for missing path", func() {
			_, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "set",
				JSON:      `{}`,
				Value:     `42`,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("delete (sjson)", func() {
		It("removes a key from JSON", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "delete",
				JSON:      `{"name": "Alice", "age": 30}`,
				Path:      "age",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).NotTo(ContainSubstring("age"))
			Expect(resp.Result).To(ContainSubstring("Alice"))
		})

		It("removes a nested key", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "delete",
				JSON:      `{"user": {"name": "Alice", "email": "a@b.c"}}`,
				Path:      "user.email",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).NotTo(ContainSubstring("email"))
			Expect(resp.Result).To(ContainSubstring("Alice"))
		})
	})

	Describe("format", func() {
		It("pretty-prints JSON", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "format",
				JSON:      `{"a":1,"b":2}`,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(ContainSubstring("\n"))
			Expect(resp.Result).To(ContainSubstring(`"a": 1`))
		})

		It("returns error for invalid JSON", func() {
			_, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "format",
				JSON:      `{invalid}`,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("minify", func() {
		It("compresses JSON to single line", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "minify",
				JSON:      "{\n  \"a\": 1,\n  \"b\": 2\n}",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).NotTo(ContainSubstring("\n"))
			Expect(resp.Result).To(Equal(`{"a":1,"b":2}`))
		})
	})

	Describe("diff_keys", func() {
		It("identifies key differences between two objects", func() {
			resp, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "diff_keys",
				JSON:      `{"a": 1, "b": 2, "c": 3}`,
				JSON2:     `{"b": 20, "c": 30, "d": 40}`,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Result).To(ContainSubstring(`"only_in_first"`))
			Expect(resp.Result).To(ContainSubstring(`"a"`))
			Expect(resp.Result).To(ContainSubstring(`"only_in_second"`))
			Expect(resp.Result).To(ContainSubstring(`"d"`))
			Expect(resp.Message).To(ContainSubstring("1 only in first"))
			Expect(resp.Message).To(ContainSubstring("1 only in second"))
			Expect(resp.Message).To(ContainSubstring("2 common"))
		})

		It("returns error when json2 is missing", func() {
			_, err := j.jsonOp(context.Background(), jsonRequest{
				Operation: "diff_keys",
				JSON:      `{"a": 1}`,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	It("returns error for empty JSON input", func() {
		_, err := j.jsonOp(context.Background(), jsonRequest{Operation: "validate"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("json input is required"))
	})

	It("returns error for unsupported operation", func() {
		_, err := j.jsonOp(context.Background(), jsonRequest{Operation: "transform", JSON: `{}`})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported operation"))
	})
})

var _ = Describe("JSON ToolProvider", func() {
	It("returns the expected tool", func() {
		p := NewToolProvider()
		tools := p.GetTools()
		Expect(tools).To(HaveLen(1))
		Expect(tools[0].Declaration().Name).To(Equal("util_json"))
	})
})
