package overlay

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/object"
)

func TestOverlay(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "overlay")
}

func obj(src, file string, folder int) *object.Object {
	var doc yaml.Node
	ExpectWithOffset(1, yaml.Unmarshal([]byte(src), &doc)).To(Succeed())
	o, err := object.New(&doc, file, ".", folder, "{{")
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return o
}

func encode(o *object.Object) string {
	out, err := yaml.Marshal(o.Body())
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return string(out)
}

const base = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n"

var _ = Describe("Merge", func() {
	It("applies JSON Merge Patch semantics", func() {
		b := obj(base+"data:\n  keep: k\n  replace: old\n  drop: x\n  list: [1, 2]\n  nested:\n    a: 1\n", "base/a.yaml", 0)
		p := obj(base+"data:\n  replace: new\n  drop: null\n  list: [3]\n  nested:\n    b: 2\n  added: y\n", "prod/a.yaml", 1)
		out, err := Merge([]*object.Object{b, p}, "{{")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(HaveLen(1))
		Expect(encode(out[0])).To(Equal(base + "data:\n    keep: k\n    replace: new\n    list: [3]\n    nested:\n        a: 1\n        b: 2\n    added: y\n"))
	})

	It("keeps the base position and appends overlay-only objects", func() {
		a := obj(base, "base/a.yaml", 0)
		x := obj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n", "base/x.yaml", 0)
		newer := obj("apiVersion: v1\nkind: Secret\nmetadata:\n  name: s\n", "prod/s.yaml", 1)
		a2 := obj(base+"data:\n  k: v\n", "prod/a.yaml", 1)
		out, err := Merge([]*object.Object{a, x, newer, a2}, "{{")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal([]*object.Object{a, x, newer}))
	})

	It("records the origin of copied nodes for error locations", func() {
		b := obj(base, "base/a.yaml", 0)
		p := obj(base+"data:\n  k: v\n", "prod/a.yaml", 1)
		out, err := Merge([]*object.Object{b, p}, "{{")
		Expect(err).NotTo(HaveOccurred())
		data := out[0].Body().Content[len(out[0].Body().Content)-1]
		Expect(out[0].Location(data)).To(HavePrefix("prod/a.yaml:"))
		Expect(out[0].Location(out[0].Body())).To(HavePrefix("base/a.yaml:"))
	})

	It("rejects duplicates inside the same folder", func() {
		_, err := Merge([]*object.Object{obj(base, "base/a.yaml", 0), obj(base, "base/b.yaml", 0)}, "{{")
		Expect(err).To(MatchError("duplicate object ConfigMap/a: base/a.yaml:1 and base/b.yaml:1"))
	})

	It("re-analyses flags after merging", func() {
		b := obj(base, "base/a.yaml", 0)
		p := obj(base+"  annotations:\n    ktpl.io/local: \"true\"\n", "prod/a.yaml", 1)
		out, err := Merge([]*object.Object{b, p}, "{{")
		Expect(err).NotTo(HaveOccurred())
		Expect(out[0].Local).To(BeTrue())
	})

	It("never merges floating objects", func() {
		f1 := obj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: '{{ \"a\" }}'\n", "base/a.yaml", 0)
		f2 := obj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: '{{ \"a\" }}'\n", "base/b.yaml", 0)
		out, err := Merge([]*object.Object{f1, f2}, "{{")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(HaveLen(2))
	})
})
