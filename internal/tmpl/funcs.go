package tmpl

import (
	"bytes"
	"errors"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"go.yaml.in/yaml/v3"
)

// alwaysRemoved lists functions that violate the zero-infrastructure invariant (network access).
var alwaysRemoved = []string{"getHostByName"}

// nonHermetic lists functions whose result depends on time, randomness or the environment.
// They are removed by --hermetic.
var nonHermetic = []string{
	// time
	"now", "date", "dateInZone", "date_in_zone", "dateModify", "date_modify", "mustDateModify",
	"must_date_modify", "htmlDate", "htmlDateInZone", "ago", "unixEpoch",
	// randomness
	"randAlphaNum", "randAlpha", "randAscii", "randNumeric", "randBytes", "randInt", "uuidv4",
	"shuffle", "bcrypt", "htpasswd", "encryptAES",
	"genPrivateKey", "genCA", "genCAWithKey", "genSelfSignedCert", "genSelfSignedCertWithKey",
	"genSignedCert", "genSignedCertWithKey",
	// environment
	"env", "expandenv",
}

// FuncMap returns Sprig plus the Helm-compatible extras, without `ref` (bound per template).
func FuncMap(hermetic bool) template.FuncMap {
	f := sprig.TxtFuncMap()
	for _, name := range alwaysRemoved {
		delete(f, name)
	}
	for name, fn := range extraFuncs() {
		if _, exists := f[name]; !exists {
			f[name] = fn
		}
	}
	if hermetic {
		for _, name := range nonHermetic {
			delete(f, name)
		}
	}
	return f
}

func extraFuncs() template.FuncMap {
	return template.FuncMap{
		"toYaml":        toYaml,
		"fromYaml":      fromYaml,
		"fromYamlArray": fromYamlArray,
		"required":      required,
	}
}

// toYaml encodes v as YAML (indent 2) without the trailing newline, like Helm.
func toYaml(v any) (string, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

func fromYaml(s string) (map[string]any, error) {
	m := map[string]any{}
	if err := yaml.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func fromYamlArray(s string) ([]any, error) {
	var a []any
	if err := yaml.Unmarshal([]byte(s), &a); err != nil {
		return nil, err
	}
	return a, nil
}

// required fails with msg when v is nil or an empty string, like Helm.
func required(msg string, v any) (any, error) {
	if v == nil {
		return nil, errors.New(msg)
	}
	if s, ok := v.(string); ok && s == "" {
		return nil, errors.New(msg)
	}
	return v, nil
}
