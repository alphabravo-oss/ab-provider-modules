package gcp

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
	if err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, r := range rs {
		if r.Type == "gcp.instance" && r.Attrs["compute_instance_public_ip"] == true {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("want instance with public IP from InstanceList, got %+v", rs)
	}
}

func TestDiscoverBucketsIAMSQLKMSFirewall(t *testing.T) {
	raw := []byte(`{"items":[{"name":"web","id":1}],"buckets":[{"name":"logs","iamConfiguration":{"publicAccessPrevention":"enforced"},"encryption":{"defaultKmsKeyName":"projects/p/locations/g/keyRings/r/cryptoKeys/k"}}],"firewalls":[{"name":"allow-ssh","sourceRanges":["0.0.0.0/0"],"allowed":[{"IPProtocol":"tcp","ports":["22"]}]}],"sql":[{"name":"db","settings":{"ipConfiguration":{"ipv4Enabled":false}}}],"kms":[{"name":"key","rotationPeriod":"2592000s"}],"iam":{"bindings":[{"role":"roles/owner","members":["user:a@b"]}]}}`)
	rs, err := DiscoverJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var bucket, fw, sql, kms, iam bool
	for _, r := range rs {
		switch r.Type {
		case "gcp.bucket":
			bucket = r.Attrs["public_access_prevention"] == "enforced" && r.Attrs["default_kms_key"] != ""
		case "gcp.firewall":
			fw = true
		case "gcp.sql":
			sql = r.Attrs["ipv4_enabled"] == false
		case "gcp.kms":
			kms = r.Attrs["rotation_period"] == "2592000s"
		case "gcp.iam":
			iam = true
		}
	}
	if !bucket || !fw || !sql || !kms || !iam {
		t.Fatalf("want bucket+firewall+sql+kms+iam from API JSON, got %+v", rs)
	}
}
