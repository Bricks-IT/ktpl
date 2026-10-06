package tmpl

import (
	"errors"
	"sync"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTmpl(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "tmpl")
}

type fakeResolver map[string]any

func (f fakeResolver) Ref(id string, p ...string) (any, error) {
	key := id
	if len(p) > 0 {
		key += " " + p[0]
	}
	v, ok := f[key]
	if !ok {
		return nil, errors.New("not found: " + key)
	}
	return v, nil
}

type errResolver struct{ err error }

func (e errResolver) Ref(string, ...string) (any, error) { return nil, e.err }

func run(c *Compiler, src string, r Resolver) (Result, error) {
	t, err := c.Compile("f", src)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return t.Execute(r)
}

var _ = Describe("typing", func() {
	res := fakeResolver{
		"a/b data.replicas": "3",
		"a/b spec.port":     8080,
		"a/b labels":        map[string]any{"app": "api"},
	}

	DescribeTable("single actions keep their native type, anything else is a string",
		func(src string, want any, typed bool) {
			got, err := run(NewCompiler(Options{}), src, res)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Value).To(Equal(want))
			Expect(got.Typed).To(Equal(typed))
		},
		Entry("string ref stays a string", `{{ ref "a/b" "data.replicas" }}`, "3", true),
		Entry("sprig cast", `{{ ref "a/b" "data.replicas" | int }}`, 3, true),
		Entry("int ref stays an int", `{{ ref "a/b" "spec.port" }}`, 8080, true),
		Entry("toString forces a string", `{{ ref "a/b" "spec.port" | toString }}`, "8080", true),
		Entry("interpolation", `port-{{ ref "a/b" "spec.port" }}`, "port-8080", false),
		Entry("surrounding space", ` {{ ref "a/b" "spec.port" }}`, " 8080", false),
		Entry("map", `{{ ref "a/b" "labels" }}`, map[string]any{"app": "api"}, true),
		Entry("trim markers", `{{- ref "a/b" "spec.port" -}}`, 8080, true),
		Entry("control structure", `{{ if true }}x{{ end }}`, "x", false),
		Entry("variable declaration", `{{ $x := 1 }}{{ $x }}`, "1", false),
		Entry("bool literal", `{{ true }}`, true, true),
		Entry("two actions", `{{ "a" }}{{ "b" }}`, "ab", false),
		Entry("toYaml", `{{ dict "k" "v" | toYaml }}`, "k: v", true),
		Entry("fromYaml", `{{ fromYaml "a: 1" }}`, map[string]any{"a": 1}, true),
	)
})

var _ = Describe("errors", func() {
	It("propagates resolver errors so errors.Is works", func() {
		sentinel := errors.New("sentinel")
		_, err := run(NewCompiler(Options{}), `x{{ ref "a/b" }}`, errResolver{sentinel})
		Expect(err).To(MatchError(sentinel))
	})
	It("rejects unknown functions at compile time", func() {
		_, err := NewCompiler(Options{}).Compile("f", "{{ nope }}")
		Expect(err).To(MatchError(ContainSubstring(`function "nope" not defined`)))
	})
	It("implements Helm's required", func() {
		_, err := run(NewCompiler(Options{}), `{{ required "boom" "" }}`, fakeResolver{})
		Expect(err).To(MatchError(ContainSubstring("boom")))
	})
})

var _ = Describe("delimiters", func() {
	It("uses custom delimiters and leaves the default ones untouched", func() {
		got, err := run(NewCompiler(Options{LeftDelim: "[[", RightDelim: "]]"}), `{{ keep }} [[ "x" | upper ]]`, fakeResolver{})
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Value).To(Equal("{{ keep }} X"))
	})
})

var _ = Describe("function map", func() {
	It("ships full Sprig by default", func() {
		Expect(FuncMap(false)).To(HaveKey("now"))
		Expect(FuncMap(false)).To(HaveKey("fromJson"))
	})
	It("always removes network functions", func() {
		Expect(FuncMap(false)).NotTo(HaveKey("getHostByName"))
	})
	It("removes non-deterministic functions in hermetic mode", func() {
		h := FuncMap(true)
		for _, name := range []string{"now", "randAlphaNum", "env", "uuidv4", "genPrivateKey"} {
			Expect(h).NotTo(HaveKey(name))
		}
		Expect(h).To(HaveKey("upper"))
	})

	It("safely serializes concurrent Execute calls", func() {
		t, err := NewCompiler(Options{}).Compile("f", `{{ ref "a/b" "v" }}`)
		Expect(err).NotTo(HaveOccurred())

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(val int) {
				defer wg.Done()
				res, err := t.Execute(fakeResolver{"a/b v": val})
				Expect(err).NotTo(HaveOccurred())
				Expect(res.Value).To(Equal(val))
			}(i)
		}
		wg.Wait()
	})
})
