package lint

import (
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/object"
	"github.com/bricks-it/ktpl/internal/path"
)

func TestLint(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "lint")
}

func obj(src string) *object.Object {
	var doc yaml.Node
	ExpectWithOffset(1, yaml.Unmarshal([]byte(src), &doc)).To(Succeed())
	o, err := object.New(&doc, "t.yaml", ".", 0, "{{")
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return o
}

func messages(errs []Error) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.String()
	}
	return out
}

const header = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n"

var _ = Describe("rules", func() {
	DescribeTable("valid objects produce no error",
		func(src string) {
			Expect(Run(nil, []*object.Object{obj(src)})).To(BeEmpty())
		},
		Entry("minimal", header+"  name: a\n"),
		Entry("labels and annotations", header+"  name: a.b-c\n  namespace: ns\n  labels:\n    app.kubernetes.io/name: api\n    empty: \"\"\n  annotations:\n    example.com/x: \"any value / with spaces\"\n"),
		Entry("RBAC names with colons", "apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: system:aggregate-to-admin\n"),
	)

	DescribeTable("invalid objects",
		func(src, want string) {
			Expect(messages(Run(nil, []*object.Object{obj(src)}))).To(ContainElement(ContainSubstring(want)))
		},
		Entry("uppercase name", header+"  name: Api\n", `metadata.name "Api" must be a valid RFC 1123 subdomain`),
		Entry("namespace with dot", header+"  name: a\n  namespace: a.b\n", `metadata.namespace "a.b" must be a valid RFC 1123 label`),
		Entry("map as label value", header+"  name: a\n  labels:\n    x:\n      y: z\n", "label value must be a string, got map"),
		Entry("float label value", header+"  name: a\n  labels:\n    v: 1.0\n", "label value must be a string, got float"),
		Entry("invalid label value", header+"  name: a\n  labels:\n    v: \"-x\"\n", `label value "-x" must be empty or consist of`),
		Entry("invalid label key", header+"  name: a\n  labels:\n    -x: y\n", `label key "-x": name part`),
		Entry("int annotation", header+"  name: a\n  annotations:\n    a: 1\n", "annotation value must be a string, got int"),
		Entry("labels not a map", header+"  name: a\n  labels: [a]\n", "labels must be a mapping, got list"),
	)

	It("locates errors with identity, path and file:line", func() {
		errs := Run(nil, []*object.Object{obj(header + "  name: a\n  namespace: demo\n  labels:\n    x:\n      y: z\n")})
		Expect(messages(errs)).To(ConsistOf("demo/ConfigMap/a metadata.labels.x (t.yaml:8): label value must be a string, got map"))
	})

	It("skips pending values", func() {
		o := obj(header + "  name: a\n  labels:\n    x: '{{ dict }}'\n")
		ctx := &State{IsPending: func(_ *object.Object, p path.Path) bool {
			return p.Overlaps(path.MustParse("metadata.labels.x"))
		}}
		Expect(Run(ctx, []*object.Object{o})).To(BeEmpty())
	})

	It("detects duplicate identities", func() {
		a, b := obj(header+"  name: a\n"), obj(header+"  name: a\n")
		Expect(messages(Run(nil, []*object.Object{a, b}))).To(ConsistOf(ContainSubstring("duplicate object identity, also defined at t.yaml:1")))
	})

	It("ignores pending names in duplicate identity checks", func() {
		floating1 := obj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: '{{ name }}'\n")
		floating2 := obj("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: '{{ name }}'\n")
		ctx := &State{IsPending: func(_ *object.Object, _ path.Path) bool {
			return true
		}}
		Expect(messages(Run(ctx, []*object.Object{floating1, floating2}))).To(BeEmpty())
	})

	It("exposes rule names", func() {
		for _, r := range Rules() {
			Expect(r.Name()).NotTo(BeEmpty())
		}
		for _, r := range GlobalRules() {
			Expect(r.Name()).NotTo(BeEmpty())
		}
	})

	It("flags structure rule violations on mutated nodes", func() {
		o := obj(header + "  name: a\n")
		// Remove apiVersion
		o.Body().Content = o.Body().Content[2:]
		errs := messages(Run(nil, []*object.Object{o}))
		Expect(errs).To(ContainElement(ContainSubstring("apiVersion must be a non-empty string")))
	})
})

var _ = Describe("name validators", func() {
	DescribeTable("qualified names",
		func(s string, valid bool) {
			Expect(validQualifiedName(s) == "").To(Equal(valid))
		},
		Entry(nil, "app", true),
		Entry(nil, "app.kubernetes.io/name", true),
		Entry(nil, "ktpl.io/rendered", true),
		Entry(nil, "a/b/c", false),
		Entry(nil, "/x", false),
		Entry(nil, "UPPER.io/x", false),
		Entry(nil, "x_", false),
	)

	It("validates DNS labels, subdomains, path segments and label values", func() {
		Expect(ValidDNS1123Label("valid-label")).To(BeEmpty())
		Expect(ValidDNS1123Label("toolong" + strings.Repeat("x", 60))).To(ContainSubstring("no more than 63 characters"))

		Expect(ValidDNS1123Subdomain("valid.subdomain.com")).To(BeEmpty())
		Expect(ValidDNS1123Subdomain("toolong" + strings.Repeat("x", 250))).To(ContainSubstring("no more than 253 characters"))

		Expect(ValidPathSegmentName(".")).To(ContainSubstring("must not be '.' or '..'"))
		Expect(ValidPathSegmentName("..")).To(ContainSubstring("must not be '.' or '..'"))
		Expect(ValidPathSegmentName("a/b")).To(ContainSubstring("must not contain '/' or '%'"))
		Expect(ValidPathSegmentName("a%b")).To(ContainSubstring("must not contain '/' or '%'"))
		Expect(ValidPathSegmentName("ok-name")).To(BeEmpty())

		Expect(validLabelValue("ok-val")).To(BeEmpty())
		Expect(validLabelValue("toolong" + strings.Repeat("x", 60))).To(ContainSubstring("no more than 63 characters"))

		Expect(validQualifiedName("toolong" + strings.Repeat("x", 250) + "/name")).To(ContainSubstring("prefix"))
		Expect(validQualifiedName("prefix/toolong" + strings.Repeat("x", 60))).To(ContainSubstring("name part must be no more than 63 characters"))
		Expect(validQualifiedName("prefix/")).To(ContainSubstring("name part must not be empty"))
	})
})
