package github

import "testing"

func TestHiddenCheckError(t *testing.T) {
	no, yes := false, true
	flags := errorExtensions{SAMLFailure: &no, IPAllowListFailure: &no}
	cases := []struct {
		name string
		typ  string
		path []any
		ext  errorExtensions
		want bool
	}{
		{"check path", "FORBIDDEN", []any{"search", "nodes", float64(0), "commits", "nodes", float64(0), "commit", "statusCheckRollup", "contexts", "nodes", float64(2)}, flags, true},
		{"int elements", "FORBIDDEN", []any{"statusCheckRollup", "contexts", 3}, flags, true},
		{"non-check path", "FORBIDDEN", []any{"search", "nodes", float64(1)}, flags, false},
		{"contexts before rollup", "FORBIDDEN", []any{"contexts", "statusCheckRollup"}, flags, false},
		{"rollup without contexts", "FORBIDDEN", []any{"statusCheckRollup", "state"}, flags, false},
		{"nil path", "FORBIDDEN", nil, flags, false},
		{"other type", "INTERNAL", []any{"statusCheckRollup", "contexts"}, flags, false},
		{"saml flag", "FORBIDDEN", []any{"statusCheckRollup", "contexts"}, errorExtensions{SAMLFailure: &yes, IPAllowListFailure: &no}, false},
		{"ip allow list flag", "FORBIDDEN", []any{"statusCheckRollup", "contexts"}, errorExtensions{SAMLFailure: &no, IPAllowListFailure: &yes}, false},
	}
	for _, tc := range cases {
		e := graphQLError{Type: tc.typ, Message: "Resource not accessible by personal access token", Path: tc.path, Extensions: tc.ext}
		if got := hiddenCheckError(e); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
