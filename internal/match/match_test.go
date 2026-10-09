package match

import "testing"

func TestTerm(t *testing.T) {
	tests := []struct {
		term, text string
		want       bool
	}{
		{"go", "Senior Go Engineer", true},
		{"go", "Google", false},
		{"go", "five years ago", false},
		{"go", "Backend (Go)", true},
		{"go", "go-to-market", true}, // why the default config uses "=Go"
		{"=Go", "go the extra mile", false},
		{"=Go", "Experience with Go and Rust", true},
		{"rust", "RUST developer", true},
		{"rust", "trustworthy", false},
		{"on-site", "fully onsite role", true},
		{"on-site", "on site", true},
		{"on-site", "on-site", true},
		{"us only", "This role is US-only.", true},
		{"us only", "US  only", true},
		{"us only", "campus only", false},
		{"c++", "Modern C++ required", true},
		{"españa", "Remoto, España", true},
		{"node.js", "nodexjs", false},
	}
	for _, tt := range tests {
		term, err := Compile(tt.term)
		if err != nil {
			t.Fatal(err)
		}
		if got := term.In(tt.text); got != tt.want {
			t.Errorf("%q in %q = %v, want %v", tt.term, tt.text, got, tt.want)
		}
	}
}

func TestCompileRejectsEmpty(t *testing.T) {
	for _, raw := range []string{"", "  ", "="} {
		if _, err := Compile(raw); err == nil {
			t.Errorf("Compile(%q): want error", raw)
		}
	}
}

func TestSetFirst(t *testing.T) {
	set, err := NewSet([]string{"junior", "intern"})
	if err != nil {
		t.Fatal(err)
	}
	if name, ok := set.First("Software Intern", "x"); !ok || name != "intern" {
		t.Errorf("First = %q, %v", name, ok)
	}
	if _, ok := set.First("Internal Tools Engineer"); ok {
		t.Error(`"intern" must not match "Internal"`)
	}
}
