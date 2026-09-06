package azure

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverJSON(t *testing.T) {
	raw, err := os.ReadFile("testdata/discover.json")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := DiscoverJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var sub, storage bool
	for _, r := range rs {
		if r.Type == "cloud_account" && r.UID == "sub-1" {
			sub = true
		}
		if r.Type == "azure.storage" && r.UID != "" {
			storage = true
			if _, ok := r.Attrs["encryption_at_rest"]; ok {
				t.Fatalf("ResourceListResult has no encryption field; Attrs must omit it: %+v", r.Attrs)
			}
			if r.Attrs["name"] != "logs" {
				t.Fatalf("want name from SDK, got %+v", r.Attrs)
			}
		}
	}
	if !sub || !storage {
		t.Fatalf("want subscription+storage from ResourceListResult, got %+v", rs)
	}
}

func TestGetPropertiesSetsEncryptionFromSDK(t *testing.T) {
	raw, err := os.ReadFile("testdata/discover-getproperties.json")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := DiscoverJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rs {
		if r.Type == "azure.storage" {
			if r.Attrs["encryption_at_rest"] != true {
				t.Fatalf("GetProperties Blob.Enabled must map to encryption_at_rest, got %+v", r.Attrs)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("want azure.storage")
	}
}

func TestNoGenericDiscoverFile(t *testing.T) {
	if _, err := os.Stat("../generic.go"); err == nil {
		t.Fatal("providers/generic.go must not exist")
	}
}

func TestDiscoverSourcesDoNotHardcodeEncryptionAtRest(t *testing.T) {
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte(`"encryption_at_rest": true`)) {
			t.Errorf("%s invents encryption_at_rest:true (copy from SDK payload only)", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
