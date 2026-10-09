package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte(`# comment
JOBSTER_TEST_A=plain
export JOBSTER_TEST_B="quoted value"
JOBSTER_TEST_C='single'

JOBSTER_TEST_SET=from-file
`), 0o600)
	t.Setenv("JOBSTER_TEST_SET", "from-env")
	for _, k := range []string{"JOBSTER_TEST_A", "JOBSTER_TEST_B", "JOBSTER_TEST_C"} {
		t.Setenv(k, "") // registers cleanup
		os.Unsetenv(k)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"JOBSTER_TEST_A":   "plain",
		"JOBSTER_TEST_B":   "quoted value",
		"JOBSTER_TEST_C":   "single",
		"JOBSTER_TEST_SET": "from-env", // the real environment wins
	} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	if err := loadDotEnv(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Errorf("missing file: %v", err)
	}
	bad := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(bad, []byte("NOT A PAIR\n"), 0o600)
	if err := loadDotEnv(bad); err == nil {
		t.Error("malformed line accepted")
	}
}
