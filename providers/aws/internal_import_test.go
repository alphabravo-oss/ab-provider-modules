package aws

import (
	"os/exec"
	"testing"
)

func TestNoSierraScanInternal(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps").CombinedOutput()
	if err != nil {
		t.Fatal(string(out), err)
	}
	if stringsContains(string(out), "sierra-scan/internal") {
		t.Fatal("must not import sierra-scan/internal")
	}
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
