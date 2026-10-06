package convert

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.yaml.in/yaml/v3"
)

func TestConvert(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "convert")
}

func parse(src string) *yaml.Node {
	var doc yaml.Node
	ExpectWithOffset(1, yaml.Unmarshal([]byte(src), &doc)).To(Succeed())
	return doc.Content[0]
}

var _ = Describe("FromNode", func() {
	DescribeTable("scalars and collections",
		func(src string, want any) {
			if want == nil {
				Expect(FromNode(parse(src))).To(BeNil())
			} else {
				Expect(FromNode(parse(src))).To(Equal(want))
			}
		},
		Entry("string", "hello", "hello"),
		Entry("quoted number stays a string", `"3"`, "3"),
		Entry("int", "3", 3),
		Entry("float", "1.5", 1.5),
		Entry("bool", "true", true),
		Entry("null", "null", nil),
		Entry("timestamp kept as written", "2024-01-02", "2024-01-02"),
		Entry("map", "a: 1\nb: x", map[string]any{"a": 1, "b": "x"}),
		Entry("list", "[1, a]", []any{1, "a"}),
		Entry("alias node", "anchor: &val hello\nref: *val", map[string]any{"anchor": "hello", "ref": "hello"}),
	)

	It("handles DocumentNode and empty DocumentNode", func() {
		doc := &yaml.Node{Kind: yaml.DocumentNode}
		val, err := FromNode(doc)
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(BeNil())

		doc.Content = []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "hello"}}
		val, err = FromNode(doc)
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal("hello"))
	})

	It("returns error for unsupported node kind", func() {
		bad := &yaml.Node{Kind: 999}
		_, err := FromNode(bad)
		Expect(err).To(MatchError(ContainSubstring("unsupported node kind")))
	})
})

var _ = Describe("ToNode", func() {
	DescribeTable("tags",
		func(v any, tag string) {
			n, err := ToNode(v)
			Expect(err).NotTo(HaveOccurred())
			Expect(n.ShortTag()).To(Equal(tag))
		},
		Entry(nil, "3", "!!str"),
		Entry(nil, 3, "!!int"),
		Entry(nil, 1.5, "!!float"),
		Entry(nil, true, "!!bool"),
		Entry(nil, nil, "!!null"),
		Entry(nil, map[string]any{"a": 1}, "!!map"),
		Entry(nil, []any{1}, "!!seq"),
	)

	It("sorts map keys", func() {
		n, err := ToNode(map[string]any{"b": 1, "a": 2})
		Expect(err).NotTo(HaveOccurred())
		Expect(n.Content[0].Value).To(Equal("a"))
	})

	It("fails when encoding unencodable types", func() {
		_, err := ToNode(make(chan int))
		Expect(err).To(HaveOccurred())
	})
})
