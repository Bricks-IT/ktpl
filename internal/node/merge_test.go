package node_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/node"
)

var _ = Describe("ExpandMergeKeys", func() {
	It("merges mapping into parent and removes <<", func() {
		input := `
labels:
  <<:
    common: "true"
    tier: "backend"
  tier: "frontend"
  name: "demo"
`
		var doc yaml.Node
		Expect(yaml.Unmarshal([]byte(input), &doc)).To(Succeed())

		Expect(node.ExpandMergeKeys(&doc)).To(Succeed())

		labels := node.MapValue(doc.Content[0], "labels")
		Expect(labels).NotTo(BeNil())
		Expect(node.MapIndex(labels, "<<")).To(Equal(-1))
		Expect(node.MapValue(labels, "common").Value).To(Equal("true"))
		// Explicit key takes precedence
		Expect(node.MapValue(labels, "tier").Value).To(Equal("frontend"))
		Expect(node.MapValue(labels, "name").Value).To(Equal("demo"))
	})

	It("merges a sequence of mappings", func() {
		input := `
labels:
  <<:
    - a: "1"
    - b: "2"
  c: "3"
`
		var doc yaml.Node
		Expect(yaml.Unmarshal([]byte(input), &doc)).To(Succeed())

		Expect(node.ExpandMergeKeys(&doc)).To(Succeed())

		labels := node.MapValue(doc.Content[0], "labels")
		Expect(node.MapIndex(labels, "<<")).To(Equal(-1))
		Expect(node.MapValue(labels, "a").Value).To(Equal("1"))
		Expect(node.MapValue(labels, "b").Value).To(Equal("2"))
		Expect(node.MapValue(labels, "c").Value).To(Equal("3"))
	})

	It("ignores scalar << values (pending templates)", func() {
		input := `
labels:
  <<: '{{ ref "ns/cm/foo" "metadata.labels" }}'
  name: "demo"
`
		var doc yaml.Node
		Expect(yaml.Unmarshal([]byte(input), &doc)).To(Succeed())

		Expect(node.ExpandMergeKeys(&doc)).To(Succeed())

		labels := node.MapValue(doc.Content[0], "labels")
		Expect(node.MapIndex(labels, "<<")).To(BeNumerically(">=", 0))
		Expect(node.MapValue(labels, "name").Value).To(Equal("demo"))
	})

	It("fails if << is an invalid type", func() {
		input := `
labels:
  <<: 123
`
		var doc yaml.Node
		Expect(yaml.Unmarshal([]byte(input), &doc)).To(Succeed())

		// Replace with an int node so it's not a scalar string
		Expect(node.ExpandMergeKeys(&doc)).To(MatchError(ContainSubstring("must be a mapping or sequence of mappings")))
	})
})
