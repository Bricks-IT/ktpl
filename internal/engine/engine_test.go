package engine

import (
	"errors"
	"io"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.yaml.in/yaml/v3"

	"github.com/bricks-it/ktpl/internal/node"
	"github.com/bricks-it/ktpl/internal/object"
	"github.com/bricks-it/ktpl/internal/path"
)

func TestEngine(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "engine")
}

func load(src string) []*object.Object {
	var objs []*object.Object
	dec := yaml.NewDecoder(strings.NewReader(src))
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		o, err := object.New(&doc, "t.yaml", ".", 0, "{{")
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		objs = append(objs, o)
	}
	return objs
}

func get(o *object.Object, p string) *yaml.Node {
	n, err := path.Get(o.Body(), path.MustParse(p))
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return n
}

func opts() Options { return Options{MaxIterations: DefaultMaxIterations} }

func iterationOf(res *Result, o *object.Object, p string) int {
	for _, f := range res.Fields {
		if f.Obj == o && f.Path.String() == p {
			return f.Iteration
		}
	}
	return -1
}

const cm = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: demo\ndata:\n"

func configMap(name string, data ...string) string {
	s := strings.Replace(cm, "%s", name, 1)
	for _, d := range data {
		s += "  " + d + "\n"
	}
	return s
}

var _ = Describe("Run", func() {
	It("renders static references in iteration 1", func() {
		objs := load(configMap("a", "x: hello") + "---\n" + configMap("b", `y: '{{ ref "demo/configmap/a" "data.x" }} world'`))
		res, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Complete).To(BeTrue())
		Expect(res.Iterations).To(Equal(1))
		Expect(get(objs[1], "data.y").Value).To(Equal("hello world"))
		Expect(iterationOf(res, objs[1], "data.y")).To(Equal(1))
	})

	It("uses snapshot semantics: a value rendered in iteration k is only visible in k+1", func() {
		// c depends on b which depends on a; declared in reverse order to prove order independence.
		objs := load(configMap("c", `v: '{{ ref "demo/configmap/b" "data.v" }}c'`) + "---\n" +
			configMap("b", `v: '{{ ref "demo/configmap/a" "data.v" }}b'`) + "---\n" +
			configMap("a", "v: a"))
		res, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(get(objs[0], "data.v").Value).To(Equal("abc"))
		Expect(iterationOf(res, objs[1], "data.v")).To(Equal(1))
		Expect(iterationOf(res, objs[0], "data.v")).To(Equal(2))
	})

	It("defers when a descendant of the referenced node is pending", func() {
		objs := load(configMap("a", `x: '{{ "late" }}'`) + "---\n" + configMap("b", `copy: '{{ ref "demo/configmap/a" "data" }}'`))
		res, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(iterationOf(res, objs[1], "data.copy")).To(Equal(2))
		Expect(get(objs[1], "data.copy.x").Value).To(Equal("late"))
	})

	It("defers when an ancestor on the path is pending (typed map not rendered yet)", func() {
		objs := load(configMap("a", `m: '{{ dict "k" "v" }}'`) + "---\n" + configMap("b", `k: '{{ ref "demo/configmap/a" "data.m.k" }}'`))
		res, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(get(objs[1], "data.k").Value).To(Equal("v"))
		Expect(iterationOf(res, objs[1], "data.k")).To(Equal(2))
	})

	It("keeps native types and key order for single actions", func() {
		objs := load(configMap("a", "z: 1", "a: 2") + "---\n" + configMap("b", `m: '{{ ref "demo/configmap/a" "data" }}'`, `n: '{{ ref "demo/configmap/a" "data.z" }}'`))
		_, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		m := get(objs[1], "data.m")
		Expect(m.Kind).To(Equal(yaml.MappingNode))
		Expect(m.Content[0].Value).To(Equal("z"), "source key order is preserved")
		Expect(get(objs[1], "data.n").ShortTag()).To(Equal("!!int"))
	})

	It("never re-parses a rendered value", func() {
		objs := load(configMap("a", `x: '{{ "{{" }} keep {{ "}}" }}'`))
		res, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Iterations).To(Equal(1))
		Expect(get(objs[0], "data.x").Value).To(Equal("{{ keep }}"))
	})

	It("reports a cycle as soon as an iteration makes no progress", func() {
		objs := load(configMap("a", `x: '{{ ref "demo/configmap/b" "data.y" }}'`) + "---\n" + configMap("b", `y: '{{ ref "demo/configmap/a" "data.x" }}'`))
		_, err := Run(objs, opts())
		var pe *PendingError
		Expect(errors.As(err, &pe)).To(BeTrue())
		Expect(pe.Cycle).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("iteration 1: no progress, 2 pending field(s)")))
		Expect(err).To(MatchError(ContainSubstring("demo/ConfigMap/a data.x (t.yaml:7) <- demo/ConfigMap/b data.y")))
	})

	It("detects a self reference as a cycle", func() {
		objs := load(configMap("a", `x: '{{ ref "demo/configmap/a" "data" }}'`))
		_, err := Run(objs, opts())
		Expect(err).To(MatchError(ContainSubstring("dependency cycle")))
	})

	It("fails when the maximum number of iterations is reached", func() {
		objs := load(configMap("a", "v: a") + "---\n" +
			configMap("b", `v: '{{ ref "demo/configmap/a" "data.v" }}'`) + "---\n" +
			configMap("c", `v: '{{ ref "demo/configmap/b" "data.v" }}'`))
		o := opts()
		o.MaxIterations = 1
		_, err := Run(objs, o)
		Expect(err).To(MatchError(HavePrefix("1 pending field(s) after 1 iteration(s) (max-iterations=1):")))
	})

	It("stops after N iterations with --stop-after and leaves pending templates untouched", func() {
		objs := load(configMap("a", "v: a") + "---\n" +
			configMap("b", `v: '{{ ref "demo/configmap/a" "data.v" }}'`) + "---\n" +
			configMap("c", `v: '{{ ref "demo/configmap/b" "data.v" }}'`))
		o := opts()
		o.StopAfter = 1
		res, err := Run(objs, o)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Complete).To(BeFalse())
		Expect(get(objs[2], "data.v").Value).To(ContainSubstring("{{ ref"))
	})

	It("never lets a floating object be referenced", func() {
		objs := load(configMap(`'{{ "gen" }}'`, "x: 1") + "---\n" + configMap("b", `y: '{{ ref "demo/configmap/gen" "data.x" }}'`))
		_, err := Run(objs, opts())
		Expect(err).To(MatchError(ContainSubstring("object not found (objects with a templated metadata.name")))
	})

	It("refreshes the identity of floating objects once rendered", func() {
		objs := load(configMap(`'{{ "gen" }}'`, "x: 1"))
		_, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(objs[0].ID.Name).To(Equal("gen"))
	})

	It("reports ambiguous kinds", func() {
		objs := load("apiVersion: v1\nkind: Service\nmetadata:\n  name: api\n  namespace: demo\n---\n" +
			"apiVersion: serving.knative.dev/v1\nkind: Service\nmetadata:\n  name: api\n  namespace: demo\n---\n" +
			configMap("c", `x: '{{ ref "demo/service/api" "metadata.name" }}'`))
		_, err := Run(objs, opts())
		Expect(err).To(MatchError(ContainSubstring(`ambiguous kind "service"`)))
	})

	It("resolves kinds qualified with their group", func() {
		objs := load("apiVersion: v1\nkind: Service\nmetadata:\n  name: api\n  namespace: demo\n---\n" +
			"apiVersion: serving.knative.dev/v1\nkind: Service\nmetadata:\n  name: api\n  namespace: demo\nspec:\n  x: knative\n---\n" +
			configMap("c", `x: '{{ ref "demo/service.serving.knative.dev/api" "spec.x" }}'`))
		_, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(get(objs[2], "data.x").Value).To(Equal("knative"))
	})

	DescribeTable("located ref errors",
		func(tmpl, msg string) {
			objs := load(configMap("a", "x: 1") + "---\n" + configMap("b", "y: '"+tmpl+"'"))
			_, err := Run(objs, opts())
			Expect(err).To(MatchError(HavePrefix("iteration 1: demo/ConfigMap/b data.y (t.yaml:15): ")))
			Expect(err).To(MatchError(ContainSubstring(msg)))
		},
		Entry("missing object", `{{ ref "demo/configmap/nope" "data" }}`, `ref "demo/configmap/nope": object not found`),
		Entry("missing path", `{{ ref "demo/configmap/a" "data.nope" }}`, `ref "demo/configmap/a" "data.nope": key "nope" does not exist`),
		Entry("bad identity", `{{ ref "nope" "data" }}`, `invalid identity "nope"`),
		Entry("bad path", `{{ ref "demo/configmap/a" "data..x" }}`, `invalid path "data..x"`),
	)

	It("skips objects annotated ktpl.io/ignore", func() {
		objs := load("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n  namespace: demo\n  annotations:\n    ktpl.io/ignore: \"true\"\ndata:\n  x: '{{ ref \"demo/configmap/nope\" \"\" }}'\n")
		res, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Fields).To(BeEmpty())
		Expect(get(objs[0], "data.x").Value).To(HavePrefix("{{ ref"))
	})

	It("skips subtrees listed in ktpl.io/ignore-key", func() {
		src := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n  namespace: demo\n  annotations:\n    ktpl.io/ignore-key: '[\"data.raw\"]'\ndata:\n  raw: '{{ .x }}'\n  ok: '{{ \"y\" }}'\n"
		objs := load(src)
		res, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(get(objs[0], "data.raw").Value).To(Equal("{{ .x }}"))
		Expect(get(objs[0], "data.ok").Value).To(Equal("y"))
		Expect(res.Fields).To(HaveLen(1))
	})

	It("fails fast on lint errors with the iteration number", func() {
		objs := load("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n  namespace: demo\n  labels:\n    bad: '{{ dict \"k\" \"v\" }}'\n")
		_, err := Run(objs, opts())
		Expect(err).To(MatchError(HavePrefix("iteration 1: lint failed, 1 error(s):")))
		Expect(err).To(MatchError(ContainSubstring("label value must be a string, got map")))
	})

	It("notifies the observer and lets it stop the run", func() {
		objs := load(configMap("a", "v: a") + "---\n" +
			configMap("b", `v: '{{ ref "demo/configmap/a" "data.v" }}'`) + "---\n" +
			configMap("c", `v: '{{ ref "demo/configmap/b" "data.v" }}'`))
		obs := &recordingObserver{stopAt: 1}
		o := opts()
		o.Observer = obs
		res, err := Run(objs, o)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Complete).To(BeFalse())
		Expect(obs.reports).To(HaveLen(1))
		Expect(obs.reports[0].Rendered).To(HaveLen(1))
		Expect(obs.reports[0].Pending).To(HaveLen(1))
		Expect(node.IsString(obs.reports[0].Pending[0].Node)).To(BeTrue())
	})

	It("merges labels across objects using << and ref", func() {
		src := `apiVersion: v1
kind: ConfigMap
metadata:
  name: common
  namespace: demo
  labels:
    app.kubernetes.io/instance: argocd
    app.kubernetes.io/part-of: argocd
data:
  v: "1"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app
  namespace: demo
  labels:
    <<: '{{ ref "demo/configmap/common" "metadata.labels" }}'
    mylabel: "2"
data:
  v: "2"
`
		objs := load(src)
		res, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Complete).To(BeTrue())

		appLabels := node.MapValue(node.MapValue(objs[1].Body(), "metadata"), "labels")
		Expect(node.MapIndex(appLabels, "<<")).To(Equal(-1))
		Expect(node.MapValue(appLabels, "mylabel").Value).To(Equal("2"))
		Expect(node.MapValue(appLabels, "app.kubernetes.io/instance").Value).To(Equal("argocd"))
		Expect(node.MapValue(appLabels, "app.kubernetes.io/part-of").Value).To(Equal("argocd"))
	})

	It("defers merge keys until referenced mapping is rendered", func() {
		src := `apiVersion: v1
kind: ConfigMap
metadata:
  name: param
  namespace: demo
data:
  version: "v1.2.3"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: common
  namespace: demo
  labels:
    app.kubernetes.io/version: '{{ ref "demo/configmap/param" "data.version" }}'
data:
  v: "1"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app
  namespace: demo
  labels:
    <<: '{{ ref "demo/configmap/common" "metadata.labels" }}'
    tier: "frontend"
data:
  v: "2"
`
		objs := load(src)
		res, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Complete).To(BeTrue())
		Expect(res.Iterations).To(Equal(2))

		appLabels := node.MapValue(node.MapValue(objs[2].Body(), "metadata"), "labels")
		Expect(node.MapIndex(appLabels, "<<")).To(Equal(-1))
		Expect(node.MapValue(appLabels, "tier").Value).To(Equal("frontend"))
		Expect(node.MapValue(appLabels, "app.kubernetes.io/version").Value).To(Equal("v1.2.3"))
	})

	It("splices sequence items across objects using - <<: and ref", func() {
		src := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: base
  namespace: demo
spec:
  template:
    spec:
      containers:
        - name: sidecar1
          image: sidecar1:v1
        - name: sidecar2
          image: sidecar2:v1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: demo
spec:
  template:
    spec:
      containers:
        - name: main
          image: main:v1
        - <<: '{{ ref "demo/deployment/base" "spec.template.spec.containers" }}'
        - name: metrics
          image: metrics:v1
`
		objs := load(src)
		res, err := Run(objs, opts())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Complete).To(BeTrue())

		containers := get(objs[1], "spec.template.spec.containers")
		Expect(containers.Content).To(HaveLen(4))
		Expect(node.MapValue(containers.Content[0], "name").Value).To(Equal("main"))
		Expect(node.MapValue(containers.Content[1], "name").Value).To(Equal("sidecar1"))
		Expect(node.MapValue(containers.Content[2], "name").Value).To(Equal("sidecar2"))
		Expect(node.MapValue(containers.Content[3], "name").Value).To(Equal("metrics"))
	})

	It("rejects an invalid maximum", func() {
		_, err := Run(nil, Options{})
		Expect(err).To(MatchError(ContainSubstring("max-iterations must be >= 1")))
	})
})

type recordingObserver struct {
	stopAt  int
	reports []*IterationReport
}

func (r *recordingObserver) Iteration(rep *IterationReport) (bool, error) {
	r.reports = append(r.reports, rep)
	return rep.Iteration == r.stopAt, nil
}
