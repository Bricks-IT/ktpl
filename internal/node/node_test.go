package node

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.yaml.in/yaml/v3"
)

func TestNode(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "node")
}

var _ = Describe("Node Helpers", func() {
	It("DeepCopy handles nil, scalars, mappings, and sequences", func() {
		Expect(DeepCopy(nil)).To(BeNil())

		s := String("hello")
		cp := DeepCopy(s)
		Expect(cp).NotTo(BeNil())
		Expect(cp.Value).To(Equal("hello"))
		Expect(cp).NotTo(BeIdenticalTo(s))

		m := Mapping()
		SetMapValue(m, "key", String("val"))
		mcp := DeepCopy(m)
		Expect(MapValue(mcp, "key").Value).To(Equal("val"))
		Expect(mcp).NotTo(BeIdenticalTo(m))
	})

	It("DeepCopy handles circular node references without stack overflow", func() {
		cyclic := &yaml.Node{Kind: yaml.SequenceNode}
		cyclic.Content = []*yaml.Node{cyclic}

		cp := DeepCopy(cyclic)
		Expect(cp).NotTo(BeNil())
		Expect(len(cp.Content)).To(Equal(1))
		Expect(cp.Content[0]).To(BeIdenticalTo(cp))
	})

	It("Walk handles nil and circular references safely", func() {
		Walk(nil, func(_ *yaml.Node) {
			Fail("should not be called for nil")
		})

		cyclic := &yaml.Node{Kind: yaml.SequenceNode}
		cyclic.Content = []*yaml.Node{cyclic}

		count := 0
		Walk(cyclic, func(_ *yaml.Node) {
			count++
		})
		Expect(count).To(Equal(1))
	})

	It("MapIndex, MapValue, MapKey and SetMapValue operate on mappings", func() {
		Expect(MapIndex(nil, "k")).To(Equal(-1))
		Expect(MapIndex(String("not a map"), "k")).To(Equal(-1))
		Expect(MapValue(nil, "k")).To(BeNil())
		Expect(MapKey(nil, "k")).To(BeNil())

		m := Mapping()
		Expect(MapIndex(m, "k")).To(Equal(-1))
		Expect(MapValue(m, "k")).To(BeNil())
		Expect(MapKey(m, "k")).To(BeNil())

		SetMapValue(m, "foo", String("bar"))
		Expect(MapIndex(m, "foo")).To(Equal(0))
		Expect(MapKey(m, "foo").Value).To(Equal("foo"))
		Expect(MapValue(m, "foo").Value).To(Equal("bar"))

		// Overwrite existing key
		SetMapValue(m, "foo", String("baz"))
		Expect(MapValue(m, "foo").Value).To(Equal("baz"))
		Expect(len(m.Content)).To(Equal(2))
	})

	It("KindName, IsString and IsNull return correct type descriptions", func() {
		Expect(KindName(nil)).To(Equal("nothing"))
		Expect(KindName(String("text"))).To(Equal("string"))
		Expect(KindName(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int"})).To(Equal("int"))
		Expect(KindName(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float"})).To(Equal("float"))
		Expect(KindName(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool"})).To(Equal("bool"))
		Expect(KindName(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"})).To(Equal("null"))
		Expect(KindName(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!custom"})).To(Equal("!!custom"))
		Expect(KindName(Mapping())).To(Equal("map"))
		Expect(KindName(&yaml.Node{Kind: yaml.SequenceNode})).To(Equal("list"))
		Expect(KindName(&yaml.Node{Kind: yaml.AliasNode, Alias: String("aliased")})).To(Equal("string"))
		Expect(KindName(&yaml.Node{Kind: yaml.DocumentNode})).To(Equal("unknown"))

		Expect(IsString(String("yes"))).To(BeTrue())
		Expect(IsString(Mapping())).To(BeFalse())
		Expect(IsString(nil)).To(BeFalse())

		Expect(IsNull(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"})).To(BeTrue())
		Expect(IsNull(String("no"))).To(BeFalse())
		Expect(IsNull(nil)).To(BeFalse())
	})

	It("ClearComments and SetLine mutate nodes", func() {
		n := String("val")
		n.HeadComment = "head"
		n.LineComment = "line"
		n.FootComment = "foot"
		ClearComments(n)
		Expect(n.HeadComment).To(BeEmpty())
		Expect(n.LineComment).To(BeEmpty())
		Expect(n.FootComment).To(BeEmpty())

		SetLine(n, 42, 10)
		Expect(n.Line).To(Equal(42))
		Expect(n.Column).To(Equal(10))
	})

	Describe("ExpandAliases", func() {
		It("expands standard alias targets", func() {
			src := "a: &x\n  k: v\nb: *x\n"
			var doc yaml.Node
			Expect(yaml.Unmarshal([]byte(src), &doc)).To(Succeed())

			Expect(ExpandAliases(&doc)).To(Succeed())
			Expect(doc.Anchor).To(BeEmpty())
			bVal := MapValue(doc.Content[0], "b")
			Expect(bVal.Kind).To(Equal(yaml.MappingNode))
			Expect(MapValue(bVal, "k").Value).To(Equal("v"))
		})

		It("detects circular alias references and returns an error", func() {
			a := &yaml.Node{Kind: yaml.AliasNode, Line: 1}
			b := &yaml.Node{Kind: yaml.AliasNode, Line: 2}
			a.Alias = b
			b.Alias = a

			root := &yaml.Node{
				Kind:    yaml.SequenceNode,
				Content: []*yaml.Node{a},
			}
			err := ExpandAliases(root)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("circular alias reference"))
		})

		It("handles nil gracefully", func() {
			Expect(ExpandAliases(nil)).To(Succeed())
		})
	})
})
