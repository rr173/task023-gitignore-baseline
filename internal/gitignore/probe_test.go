package gitignore

import (
	"testing"
)

// TestProbeEmptyRulesKeepAll verifies that when no rules are provided,
// all submitted paths appear in the result set as kept (not ignored).
func TestProbeEmptyRulesKeepAll(t *testing.T) {
	ps := mustParse(t, "")
	paths := []string{"README.md", "src/main.go", "docs/api.txt"}
	results := Check(ps, paths)
	if len(results) != len(paths) {
		t.Fatalf("expected %d results for empty rules, got %d", len(paths), len(results))
	}
	for i, r := range results {
		if r.Ignored {
			t.Errorf("path %q at index %d should be kept with empty rules", r.Path, i)
		}
		if r.Path != paths[i] {
			t.Errorf("result[%d].Path = %q, want %q", i, r.Path, paths[i])
		}
	}
}

// TestProbeLeadingDoubleStarDeepNesting verifies that a leading ** pattern
// matches paths nested more than one directory level deep.
func TestProbeLeadingDoubleStarDeepNesting(t *testing.T) {
	ps := mustParse(t, "**/secret.key\n")
	deepPaths := []string{
		"secret.key",
		"config/secret.key",
		"deploy/staging/secret.key",
		"a/b/c/d/secret.key",
	}
	for _, p := range deepPaths {
		ignored, matched := Decide(ps, p)
		if !ignored {
			t.Errorf("path %q should be ignored by **/secret.key", p)
		}
		if matched == nil || matched.Source != "**/secret.key" {
			t.Errorf("path %q deciding rule should be **/secret.key, got %+v", p, matched)
		}
	}
}

// TestProbeParsePanicOnEdgeRule ensures that parsing rules with edge-case
// combinations of negation and directory markers does not panic.
func TestProbeParsePanicOnEdgeRule(t *testing.T) {
	edgeCases := []string{
		"!/\n",
		"!\n",
		"/\n",
	}
	for _, rule := range edgeCases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Parse(%q) panicked: %v", rule, r)
				}
			}()
			// Should not panic regardless of rule content.
			_, _ = Parse(rule)
		}()
	}
}

// TestProbeBasenameCascadeSubpath verifies that a basename rule (no slash)
// correctly cascades to ignore sub-paths underneath the matched segment.
func TestProbeBasenameCascadeSubpath(t *testing.T) {
	ps := mustParse(t, "vendor\n")
	// vendor/pkg/util.go should be ignored because "vendor" matches
	// as a path segment and the rule cascades to children.
	cases := []struct {
		path    string
		ignored bool
	}{
		{"vendor", true},
		{"vendor/pkg/util.go", true},
		{"src/vendor/lib.go", true},
		{"src/vendor/deep/file.go", true},
		{"vendorfile.txt", false},
	}
	for _, c := range cases {
		ignored, _ := Decide(ps, c.path)
		if ignored != c.ignored {
			t.Errorf("path %q: ignored=%v want %v (rule: vendor)", c.path, ignored, c.ignored)
		}
	}
}
