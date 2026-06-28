package handlers

import "testing"

func TestSafeRedirectPath(t *testing.T) {
	cases := map[string]string{
		"/dashboard":          "/dashboard",
		"/foo/bar?x=1":        "/foo/bar?x=1",
		"":                    "/",
		"//evil.com":          "/",
		"https://evil.com":    "/",
		"/\\evil.com":         "/",
		"javascript:alert(1)": "/",
		"relative/path":       "/",
	}
	for in, want := range cases {
		if got := safeRedirectPath(in); got != want {
			t.Errorf("safeRedirectPath(%q) = %q, want %q", in, got, want)
		}
	}
}
