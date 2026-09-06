package aws

import (
	"os"
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
	var bucket, img, pab, mfa bool
	for _, r := range rs {
		if r.UID == "arn:aws:s3:::payments" {
			bucket = true
			block, _ := r.Attrs["public_access_block"].(map[string]any)
			if block != nil && block["ignore_public_acls"] == false {
				pab = true
			}
		}
		if r.Type == "container_image" && strings.HasPrefix(r.UID, "sha256:") {
			img = true
		}
		if r.Type == "aws.account" {
			acct, _ := r.Attrs["account"].(map[string]any)
			if acct != nil {
				if v, ok := acct["mfa_enabled"].(bool); ok && !v {
					mfa = true
				}
			}
		}
	}
	if !bucket || !img || !pab || !mfa {
		t.Fatalf("want bucket+pab+image+account MFA from IAM summary, got %+v", rs)
	}
}

func TestNoInventedPeeringOrIAMDays(t *testing.T) {
	raw := []byte(`{"account":"1","listUsers":{"Users":[{"UserName":"bob"}]},"describeVpcs":{"Vpcs":[{"VpcId":"vpc-1"}]}}`)
	rs, _, err := Discover(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Type == "aws.vpc" {
			if _, ok := r.Attrs["peering_least_privilege"]; ok {
				t.Fatal("peering_least_privilege must not be hardcoded")
			}
		}
		if r.Type == "aws.iam_user" {
			for _, k := range []string{"access_key_last_used_days", "password_last_used_days", "access_key_during_create"} {
				if _, ok := r.Attrs[k]; ok {
					t.Fatalf("omit %s unless SDK supplies it, got %+v", k, r.Attrs)
				}
			}
		}
	}
}

func TestFailedPublicAccessBlockOmitsAttrs(t *testing.T) {
	raw := []byte(`{
	  "account":"123456789012",
	  "listBuckets":{"Buckets":[{"Name":"payments"}],"Owner":{"ID":"123456789012"}},
	  "publicAccessBlock":{"payments":{"PublicAccessBlockConfiguration":{"IgnorePublicAcls":false,"RestrictPublicBuckets":false}}},
	  "failedPublicAccessBlock":["payments"]
	}`)
	rs, errs, err := Discover(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) == 0 {
		t.Fatal("want service error for failed GetPublicAccessBlock")
	}
	for _, r := range rs {
		if r.UID == "arn:aws:s3:::payments" {
			if _, ok := r.Attrs["public_access_block"]; ok {
				t.Fatalf("failed Get must not invent PAB attrs: %+v", r.Attrs)
			}
		}
	}
}
