package validate

import "testing"

func TestName(t *testing.T) {
	valid := []string{"", "my-session", "test_123", "Session.v2", "a"}
	for _, n := range valid {
		if err := Name(n); err != nil {
			t.Errorf("Name(%q) should be valid, got: %v", n, err)
		}
	}

	invalid := []string{"-start-dash", ".dot", " space", "a/b", string(make([]byte, 200))}
	for _, n := range invalid {
		if err := Name(n); err == nil {
			t.Errorf("Name(%q) should be invalid", n)
		}
	}
}

func TestMode(t *testing.T) {
	for _, m := range []string{"explore", "execute", "ask", ""} {
		if err := Mode(m); err != nil {
			t.Errorf("Mode(%q) should be valid", m)
		}
	}
	if err := Mode("invalid"); err == nil {
		t.Error("Mode(invalid) should fail")
	}
}

func TestPackageName(t *testing.T) {
	if err := PackageName("python@3.12"); err != nil {
		t.Errorf("PackageName should accept version spec: %v", err)
	}
	if err := PackageName(""); err == nil {
		t.Error("PackageName should reject empty")
	}
}
