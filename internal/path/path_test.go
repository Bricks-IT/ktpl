package path

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.yaml.in/yaml/v3"
)

func TestPath(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "path")
}

var _ = Describe("Parse and String", func() {
	DescribeTable("valid paths round-trip to their canonical form",
		func(in string, want Path, canonical string) {
			got, err := Parse(in)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Equal(want)).To(BeTrue(), "got %#v", got)
			Expect(got.String()).To(Equal(canonical))
		},
		Entry("root", "", Path{}, ""),
		Entry("single key", "data", Path{Key("data")}, "data"),
		Entry("keys and index", "spec.ports[0].port", Path{Key("spec"), Key("ports"), Index(0), Key("port")}, "spec.ports[0].port"),
		Entry("single-quoted key", "metadata.labels['app.kubernetes.io/name']",
			Path{Key("metadata"), Key("labels"), Key("app.kubernetes.io/name")}, "metadata.labels['app.kubernetes.io/name']"),
		Entry("double-quoted key", `data["config.yaml"]`, Path{Key("data"), Key("config.yaml")}, "data['config.yaml']"),
		Entry("escaped quote", `data['it\'s']`, Path{Key("data"), Key("it's")}, `data['it\'s']`),
		Entry("leading indexes", "[0][1]", Path{Index(0), Index(1)}, "[0][1]"),
		Entry("key after index", "a[0].b", Path{Key("a"), Index(0), Key("b")}, "a[0].b"),
		Entry("underscore key", "DB_HOST", Path{Key("DB_HOST")}, "DB_HOST"),
		Entry("empty quoted key", "a['']", Path{Key("a"), Key("")}, "a['']"),
	)

	DescribeTable("invalid paths are rejected",
		func(in string) {
			_, err := Parse(in)
			Expect(err).To(MatchError(ContainSubstring("invalid path")))
		},
		Entry(nil, ".a"), Entry(nil, "a."), Entry(nil, "a..b"), Entry(nil, "a["), Entry(nil, "a[x]"),
		Entry(nil, "a[-1]"), Entry(nil, "a['x'"), Entry(nil, "a['x'b]"), Entry(nil, "a]b"),
		Entry(nil, "a[0]b"), Entry(nil, "a.[0]"),
	)
})

var _ = Describe("prefixes", func() {
	a := MustParse("spec.template")
	b := MustParse("spec.template.metadata.labels")
	c := MustParse("spec.selector")

	It("detects ancestors", func() {
		Expect(b.HasPrefix(a)).To(BeTrue())
		Expect(a.HasPrefix(b)).To(BeFalse())
	})
	It("detects overlaps in both directions", func() {
		Expect(a.Overlaps(b)).To(BeTrue())
		Expect(b.Overlaps(a)).To(BeTrue())
		Expect(a.Overlaps(c)).To(BeFalse())
		Expect(Path{}.Overlaps(c)).To(BeTrue())
	})
	It("never mutates the receiver in Child", func() {
		base := make(Path, 1, 4)
		base[0] = Key("a")
		x, y := base.Child(Key("x")), base.Child(Key("y"))
		Expect(x.String()).To(Equal("a.x"))
		Expect(y.String()).To(Equal("a.y"))
	})
})

var _ = Describe("Get", func() {
	var root *yaml.Node

	BeforeEach(func() {
		var doc yaml.Node
		src := "spec:\n  ports:\n    - name: http\n      port: 8080\nmetadata:\n  labels:\n    app.kubernetes.io/name: api\n"
		Expect(yaml.Unmarshal([]byte(src), &doc)).To(Succeed())
		root = doc.Content[0]
	})

	DescribeTable("existing nodes",
		func(p, want string) {
			n, err := Get(root, MustParse(p))
			Expect(err).NotTo(HaveOccurred())
			Expect(n.Value).To(Equal(want))
		},
		Entry(nil, "spec.ports[0].port", "8080"),
		Entry(nil, "metadata.labels['app.kubernetes.io/name']", "api"),
	)

	DescribeTable("missing nodes",
		func(p, msg string) {
			_, err := Get(root, MustParse(p))
			var nf *NotFoundError
			Expect(err).To(BeAssignableToTypeOf(nf))
			Expect(err).To(MatchError(msg))
		},
		Entry(nil, "spec.ports[1]", `path "spec.ports[1]" not found: index 1 out of range (length 1)`),
		Entry(nil, "spec.nope", `path "spec.nope" not found: key "nope" does not exist`),
		Entry(nil, "spec.ports.name", `path "spec.ports.name" not found: cannot read key "name" of list`),
		Entry(nil, "spec[0]", `path "spec[0]" not found: cannot index map with [0]`),
	)
})

var _ = Describe("Walk", func() {
	It("visits values in document order", func() {
		var doc yaml.Node
		Expect(yaml.Unmarshal([]byte("b: 1\na:\n  - x\n  - y: z\n"), &doc)).To(Succeed())
		var got []string
		Walk(doc.Content[0], func(p Path, _ *yaml.Node) bool {
			got = append(got, p.String())
			return true
		})
		Expect(got).To(Equal([]string{"b", "a", "a[0]", "a[1]", "a[1].y"}))
	})

	It("skips children when the callback returns false", func() {
		var doc yaml.Node
		Expect(yaml.Unmarshal([]byte("a:\n  b: 1\nc: 2\n"), &doc)).To(Succeed())
		var got []string
		Walk(doc.Content[0], func(p Path, _ *yaml.Node) bool {
			got = append(got, p.String())
			return false
		})
		Expect(got).To(Equal([]string{"a", "c"}))
	})
})
