package modules_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestProvidersDoNotImportSierraScanInternal(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./...").CombinedOutput()
	if err != nil {
		t.Fatal(string(out), err)
	}
	if strings.Contains(string(out), "sierra-scan/internal") {
		t.Fatal("provider modules must not import sierra-scan/internal")
	}
}

func TestNoUnusedGenericDiscover(t *testing.T) {
	if _, err := os.Stat("providers/generic.go"); err == nil {
		t.Fatal("unused DiscoverJSON helper providers/generic.go must not exist")
	}
}
