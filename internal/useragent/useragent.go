// Package useragent builds the User-Agent header the CLI sends on every
// outbound HTTP request, including the OAuth token endpoint.
package useragent

import (
	"net/http"
	"strings"
)

// product is the bare token used when the build version is unknown.
const product = "omni-cli"

var value = product

// Set records the version reported in the User-Agent header. Dev, unversioned
// ("(devel)", what the Go toolchain reports for a source build) and empty
// versions keep the bare product token.
func Set(version string) {
	version = strings.TrimSpace(version)
	if version == "" || version == "dev" || version == "(devel)" {
		value = product
		return
	}
	value = product + "/" + strings.TrimPrefix(version, "v")
}

// String returns the current User-Agent header value.
func String() string {
	return value
}

// transport stamps the CLI's User-Agent onto every request it forwards.
type transport struct {
	base http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// RoundTrippers must not modify the request they're given.
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", String())
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

// Client returns an HTTP client that sets the CLI's User-Agent on every
// request. Use it for requests built by libraries we don't control, such as
// golang.org/x/oauth2.
func Client() *http.Client {
	return &http.Client{Transport: &transport{}}
}
