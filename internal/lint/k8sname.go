package lint

import (
	"regexp"
	"strings"
)

// Kubernetes name validation, implemented locally (no k8s.io dependency).

const (
	dns1123LabelMaxLen     = 63
	dns1123SubdomainMaxLen = 253
	qualifiedNameMaxLen    = 63
	labelValueMaxLen       = 63
)

var (
	dns1123LabelRe     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	dns1123SubdomainRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
	qualifiedNameRe    = regexp.MustCompile(`^([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]$`)
	labelValueRe       = regexp.MustCompile(`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`)
)

// ValidDNS1123Label returns an explanation if s is not a valid RFC 1123 label, or "".
func ValidDNS1123Label(s string) string {
	if len(s) > dns1123LabelMaxLen {
		return "must be no more than 63 characters"
	}
	if !dns1123LabelRe.MatchString(s) {
		return "must be a valid RFC 1123 label (lowercase alphanumerics and '-', starting and ending with an alphanumeric)"
	}
	return ""
}

// ValidDNS1123Subdomain returns an explanation if s is not a valid RFC 1123 subdomain, or "".
func ValidDNS1123Subdomain(s string) string {
	if len(s) > dns1123SubdomainMaxLen {
		return "must be no more than 253 characters"
	}
	if !dns1123SubdomainRe.MatchString(s) {
		return "must be a valid RFC 1123 subdomain (lowercase alphanumerics, '-' and '.', starting and ending with an alphanumeric)"
	}
	return ""
}

// ValidPathSegmentName is the minimal rule applied to every kind (used for RBAC objects).
func ValidPathSegmentName(s string) string {
	if s == "." || s == ".." {
		return "must not be '.' or '..'"
	}
	if strings.ContainsAny(s, "/%") {
		return "must not contain '/' or '%'"
	}
	return ""
}

// validQualifiedName validates label and annotation keys: [prefix/]name.
func validQualifiedName(s string) string {
	name := s
	if prefix, n, ok := strings.Cut(s, "/"); ok {
		if prefix == "" {
			return "prefix must not be empty"
		}
		if msg := ValidDNS1123Subdomain(prefix); msg != "" {
			return "prefix " + msg
		}
		name = n
	}
	if name == "" {
		return "name part must not be empty"
	}
	if len(name) > qualifiedNameMaxLen {
		return "name part must be no more than 63 characters"
	}
	if !qualifiedNameRe.MatchString(name) {
		return "name part must consist of alphanumerics, '-', '_' or '.', starting and ending with an alphanumeric"
	}
	return ""
}

func validLabelValue(s string) string {
	if len(s) > labelValueMaxLen {
		return "must be no more than 63 characters"
	}
	if !labelValueRe.MatchString(s) {
		return "must be empty or consist of alphanumerics, '-', '_' or '.', starting and ending with an alphanumeric"
	}
	return ""
}
