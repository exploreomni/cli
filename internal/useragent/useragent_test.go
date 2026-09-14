package useragent

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSet(t *testing.T) {
	t.Cleanup(func() { Set("dev") })

	tests := []struct {
		version string
		want    string
	}{
		{"1.2.3", "omni-cli/1.2.3"},
		{"v1.2.3", "omni-cli/1.2.3"},
		{" v1.2.3 ", "omni-cli/1.2.3"},
		{"dev", "omni-cli"},
		// What debug.ReadBuildInfo reports for an unversioned source build.
		{"(devel)", "omni-cli"},
		{"", "omni-cli"},
	}
	for _, tt := range tests {
		Set(tt.version)
		if got := String(); got != tt.want {
			t.Errorf("Set(%q): String() = %q, want %q", tt.version, got, tt.want)
		}
	}
}

func TestClient_SetsUserAgent(t *testing.T) {
	Set("9.9.9")
	t.Cleanup(func() { Set("dev") })

	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
	}))
	defer srv.Close()

	req, err := http.NewRequest("GET", srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if gotUA != "omni-cli/9.9.9" {
		t.Errorf("User-Agent = %q, want %q", gotUA, "omni-cli/9.9.9")
	}
	// RoundTrippers must leave the caller's request untouched.
	if got := req.Header.Get("User-Agent"); got != "" {
		t.Errorf("caller's request was mutated: User-Agent = %q, want empty", got)
	}
}
