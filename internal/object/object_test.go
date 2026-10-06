package object

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/path"
)

func TestObject(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "object")
}

func parseDoc(src string) *yaml.Node {
	var doc yaml.Node
	ExpectWithOffset(1, yaml.Unmarshal([]byte(src), &doc)).To(Succeed())
	return &doc
}

func mustObject(src string) *Object {
	o, err := New(parseDoc(src), "f.yaml", ".", 0, "{{")
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return o
}

var _ = Describe("ParseRef", func() {
	DescribeTable("valid identities",
		func(in string, want Ref) {
			want.Raw = in
			Expect(ParseRef(in)).To(Equal(want))
		},
		Entry("namespaced", "shop/configmap/app", Ref{Namespace: "shop", Kind: "configmap", Name: "app"}),
		Entry("kind is lower-cased", "shop/ConfigMap/app", Ref{Namespace: "shop", Kind: "configmap", Name: "app"}),
		Entry("no namespace", "clusterrole/admin", Ref{Kind: "clusterrole", Name: "admin"}),
		Entry("group-qualified kind", "shop/service.serving.knative.dev/api",
			Ref{Namespace: "shop", Kind: "service", Group: "serving.knative.dev", Name: "api"}),
	)

	DescribeTable("invalid identities",
		func(in string) {
			_, err := ParseRef(in)
			Expect(err).To(MatchError(ContainSubstring("invalid identity")))
		},
		Entry("leading slash", "/clusterrole/admin"),
		Entry("one segment", "app"),
		Entry("four segments", "a/b/c/d"),
		Entry("empty kind", "shop//app"),
		Entry("empty kind before group", "shop/.group/app"),
	)
})

var _ = Describe("Ref.Matches", func() {
	svc := func() *Object {
		return mustObject("apiVersion: serving.knative.dev/v1\nkind: Service\nmetadata:\n  name: api\n  namespace: shop\n")
	}
	cr := func() *Object {
		return mustObject("apiVersion: rbac.authorization.k8s.io/v1\nkind: ClusterRole\nmetadata:\n  name: admin\n")
	}

	DescribeTable("matching",
		func(ref string, obj func() *Object, want bool) {
			r, err := ParseRef(ref)
			Expect(err).NotTo(HaveOccurred())
			Expect(r.Matches(obj())).To(Equal(want))
		},
		Entry(nil, "shop/service/api", svc, true),
		Entry(nil, "shop/service.serving.knative.dev/api", svc, true),
		Entry(nil, "shop/service.apps/api", svc, false),
		Entry(nil, "other/service/api", svc, false),
		Entry("namespace is never implied", "service/api", svc, false),
		Entry(nil, "clusterrole/admin", cr, true),
		Entry(nil, "default/clusterrole/admin", cr, false),
	)
})

var _ = Describe("New", func() {
	It("reads identity and ktpl annotations", func() {
		o := mustObject(`apiVersion: v1
kind: ConfigMap
metadata:
  name: '{{ "x" }}'
  namespace: demo
  annotations:
    ktpl.io/local: "true"
    ktpl.io/ignore-key: '["data[''a.json'']", "spec.groups"]'
`)
		Expect(o.Floating).To(BeTrue())
		Expect(o.Local).To(BeTrue())
		Expect(o.Ignore).To(BeFalse())
		Expect(o.IgnorePaths).To(HaveLen(2))
		Expect(o.IgnorePaths[0].String()).To(Equal("data['a.json']"))
		Expect(o.ID.Namespace).To(Equal("demo"))
	})

	It("honours the kustomize local-config annotation", func() {
		o := mustObject("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n  annotations:\n    config.kubernetes.io/local-config: \"true\"\n")
		Expect(o.Local).To(BeTrue())
	})

	It("computes the API group", func() {
		Expect(GroupOf("apps/v1")).To(Equal("apps"))
		Expect(GroupOf("v1")).To(BeEmpty())
		Expect(GroupOf("networking.k8s.io/v1")).To(Equal("networking.k8s.io"))
	})

	DescribeTable("rejects invalid documents with a located error",
		func(src, msg string) {
			_, err := New(parseDoc(src), "f.yaml", ".", 0, "{{")
			Expect(err).To(MatchError(And(ContainSubstring("f.yaml:"), ContainSubstring(msg))))
		},
		Entry(nil, "apiVersion: '{{ x }}'\nkind: A\nmetadata:\n  name: a\n", "apiVersion cannot be templated"),
		Entry(nil, "apiVersion: v1\nkind: '{{ x }}'\nmetadata:\n  name: a\n", "kind cannot be templated"),
		Entry(nil, "apiVersion: v1\nkind: A\n", "metadata is required"),
		Entry(nil, "apiVersion: v1\nkind: A\nmetadata: x\n", "metadata must be a mapping"),
		Entry(nil, "apiVersion: v1\nkind: A\nmetadata:\n  namespace: x\n", "metadata.name must be a non-empty string"),
		Entry(nil, "kind: A\nmetadata:\n  name: a\n", "apiVersion must be a non-empty string"),
		Entry(nil, "apiVersion: v1\nkind: A\nmetadata:\n  name: a\n  annotations:\n    ktpl.io/ignore-key: nope\n", "must be a JSON list of paths"),
		Entry(nil, "- a\n", "document must be a mapping"),
	)

	It("handles Rel, Location, RecordOrigin and FileOf", func() {
		o := mustObject("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n")
		o.File = "base/sub/app.yaml"
		o.Root = "base"
		Expect(o.Rel()).To(Equal("sub/app.yaml"))

		Expect(o.Location(nil)).To(Equal("base/sub/app.yaml:1"))
		n := o.Body()
		Expect(o.Location(n)).To(Equal("base/sub/app.yaml:1"))

		otherNode := &yaml.Node{Kind: yaml.ScalarNode, Line: 10, Value: "overlay"}
		o.RecordOrigin(otherNode, "overlays/prod/app.yaml")
		Expect(o.FileOf(otherNode)).To(Equal("overlays/prod/app.yaml"))
		Expect(o.Location(otherNode)).To(Equal("overlays/prod/app.yaml:10"))
	})

	It("handles IsIgnored and ktpl.io/ignore annotation", func() {
		o := mustObject("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n  annotations:\n    ktpl.io/ignore: \"true\"\n")
		Expect(o.Ignore).To(BeTrue())

		o2 := mustObject("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n  annotations:\n    ktpl.io/ignore-key: '[\"data.secret\"]'\n")
		Expect(o2.IsIgnored(path.MustParse("data.secret"))).To(BeTrue())
		Expect(o2.IsIgnored(path.MustParse("data.other"))).To(BeFalse())
	})

	It("handles RefreshIdentity, Reanalyze and Identity methods", func() {
		o := mustObject("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: '{{ name }}'\n  namespace: '{{ ns }}'\n")
		Expect(o.Floating).To(BeTrue())

		// Update name in body
		meta := o.Body().Content[5] // metadata mapping
		meta.Content[1].Value = "real-name"
		meta.Content[3].Value = "real-ns"

		o.RefreshIdentity()
		Expect(o.ID.Name).To(Equal("real-name"))
		Expect(o.ID.Namespace).To(Equal("real-ns"))
		Expect(o.Floating).To(BeTrue()) // remains true until reanalyze

		Expect(o.ID.String()).To(Equal("real-ns/ConfigMap/real-name"))
		Expect(o.ID.Key()).To(Equal("real-ns/configmap./real-name"))

		Expect(o.Reanalyze("{{")).To(Succeed())
		Expect(o.Floating).To(BeFalse())
	})
})
