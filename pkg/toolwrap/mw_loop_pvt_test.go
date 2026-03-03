package toolwrap

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("isEmptyResult", func() {
	It("returns true for nil", func() {
		Expect(isEmptyResult(nil)).To(BeTrue())
	})

	It("returns true for json.RawMessage with count:0 and results:[]", func() {
		raw := json.RawMessage(`{"results":[],"count":0}`)
		Expect(isEmptyResult(raw)).To(BeTrue())
	})

	It("returns false for json.RawMessage with count:3", func() {
		raw := json.RawMessage(`{"results":[{"id":"1"}],"count":3}`)
		Expect(isEmptyResult(raw)).To(BeFalse())
	})

	It("returns true for []byte with count:0", func() {
		Expect(isEmptyResult([]byte(`{"count":0}`))).To(BeTrue())
	})

	It("returns true for string with empty results", func() {
		Expect(isEmptyResult(`{"results":[]}`)).To(BeTrue())
	})

	It("returns false for string with non-empty results", func() {
		Expect(isEmptyResult(`{"results":[{"x":"y"}]}`)).To(BeFalse())
	})

	It("returns false for non-JSON string", func() {
		Expect(isEmptyResult("hello world")).To(BeFalse())
	})

	It("returns true for struct marshaling to count:0", func() {
		s := struct {
			Count   int   `json:"count"`
			Results []int `json:"results"`
		}{Count: 0, Results: []int{}}
		Expect(isEmptyResult(s)).To(BeTrue())
	})

	It("returns false for struct marshaling to count:1", func() {
		s := struct {
			Count   int   `json:"count"`
			Results []int `json:"results"`
		}{Count: 1, Results: []int{42}}
		Expect(isEmptyResult(s)).To(BeFalse())
	})

	It("returns false for map with no count or results keys", func() {
		Expect(isEmptyResult(map[string]string{"status": "ok"})).To(BeFalse())
	})

	It("returns false for unmarshalable types", func() {
		ch := make(chan int)
		Expect(isEmptyResult(ch)).To(BeFalse())
	})

	It("returns true for found:false (graph_get_entity shape)", func() {
		raw := json.RawMessage(`{"entity":null,"found":false}`)
		Expect(isEmptyResult(raw)).To(BeTrue())
	})

	It("returns false for found:true (graph_get_entity shape)", func() {
		raw := json.RawMessage(`{"entity":{"id":"1"},"found":true}`)
		Expect(isEmptyResult(raw)).To(BeFalse())
	})

	It("returns true for empty path array (graph_shortest_path shape)", func() {
		raw := json.RawMessage(`{"path":[],"found":false}`)
		Expect(isEmptyResult(raw)).To(BeTrue())
	})

	It("returns false for non-empty path array (graph_shortest_path shape)", func() {
		raw := json.RawMessage(`{"path":[{"id":"a"},{"id":"b"}],"found":true}`)
		Expect(isEmptyResult(raw)).To(BeFalse())
	})
})
