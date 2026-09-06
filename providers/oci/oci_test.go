package oci

import (
	"os"
	"testing"
)

func TestDiscoverJSON(t *testing.T) {
	raw, err := os.ReadFile("testdata/discover.json")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := DiscoverJSON(raw)
	if err != nil || len(rs) < 2 {
		t.Fatalf("%v %+v", err, rs)
	}
	if rs[1].UID != "ocid1.instance.oc1..aaaa" {
		t.Fatalf("uid %s", rs[1].UID)
	}
}
