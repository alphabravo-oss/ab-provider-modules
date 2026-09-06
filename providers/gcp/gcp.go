package gcp

import (
	"encoding/json"
	"fmt"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

func DiscoverJSON(raw []byte) ([]resource.Resource, error) {
	instRaw := raw
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) == nil {
		if items, ok := probe["items"]; ok {
			instRaw, _ = json.Marshal(map[string]json.RawMessage{"items": items})
		}
	}
	var list computepb.InstanceList
	if err := protojson.Unmarshal(instRaw, &list); err != nil {
		return nil, err
	}
	var extra struct {
		Buckets []struct {
			Name             string `json:"name"`
			IamConfiguration struct {
				PublicAccessPrevention string `json:"publicAccessPrevention"`
			} `json:"iamConfiguration"`
			Encryption struct {
				DefaultKmsKeyName string `json:"defaultKmsKeyName"`
			} `json:"encryption"`
		} `json:"buckets"`
		Firewalls []struct {
			Name         string   `json:"name"`
			SourceRanges []string `json:"sourceRanges"`
			Allowed      []struct {
				IPProtocol string   `json:"IPProtocol"`
				Ports      []string `json:"ports"`
			} `json:"allowed"`
		} `json:"firewalls"`
		SQL []struct {
			Name     string `json:"name"`
			Settings struct {
				IpConfiguration struct {
					Ipv4Enabled bool `json:"ipv4Enabled"`
				} `json:"ipConfiguration"`
			} `json:"settings"`
		} `json:"sql"`
		KMS []struct {
			Name           string `json:"name"`
			RotationPeriod string `json:"rotationPeriod"`
		} `json:"kms"`
		IAM struct {
			Bindings []struct {
				Role    string   `json:"role"`
				Members []string `json:"members"`
			} `json:"bindings"`
		} `json:"iam"`
	}
	_ = json.Unmarshal(raw, &extra)
	out := []resource.Resource{{
		Type: "cloud_account", UID: "proj-1", Account: "proj-1",
		Aliases: []resource.Alias{{Kind: "gcp_project_id", Value: "proj-1"}},
	}}
	for _, inst := range list.GetItems() {
		if inst == nil {
			continue
		}
		uid := fmt.Sprintf("//compute.googleapis.com/projects/p/zones/z/instances/%s", inst.GetName())
		pub := false
		for _, nic := range inst.GetNetworkInterfaces() {
			if nic == nil {
				continue
			}
			if len(nic.GetAccessConfigs()) > 0 {
				pub = true
			}
		}
		out = append(out, resource.Resource{
			Type: "gcp.instance", UID: uid, Account: "proj-1",
			Aliases: []resource.Alias{{Kind: "cloud_arn", Value: uid}, {Kind: "gcp_project_id", Value: "proj-1"}},
			Attrs:   map[string]any{"compute_instance_public_ip": pub, "publicly_exposed": pub},
		})
	}
	for _, b := range extra.Buckets {
		uid := "gs://" + b.Name
		out = append(out, resource.Resource{
			Type: "gcp.bucket", UID: uid, Account: "proj-1",
			Aliases: []resource.Alias{{Kind: "cloud_arn", Value: uid}, {Kind: "gcp_project_id", Value: "proj-1"}},
			Attrs: map[string]any{
				"public_access_prevention": b.IamConfiguration.PublicAccessPrevention,
				"default_kms_key":          b.Encryption.DefaultKmsKeyName,
			},
		})
	}
	for _, fw := range extra.Firewalls {
		out = append(out, resource.Resource{
			Type: "gcp.firewall", UID: fw.Name, Account: "proj-1",
			Attrs: map[string]any{"source_ranges": fw.SourceRanges, "allowed": fw.Allowed},
		})
	}
	for _, s := range extra.SQL {
		out = append(out, resource.Resource{
			Type: "gcp.sql", UID: s.Name, Account: "proj-1",
			Attrs: map[string]any{"ipv4_enabled": s.Settings.IpConfiguration.Ipv4Enabled},
		})
	}
	for _, k := range extra.KMS {
		out = append(out, resource.Resource{
			Type: "gcp.kms", UID: k.Name, Account: "proj-1",
			Attrs: map[string]any{"rotation_period": k.RotationPeriod},
		})
	}
	if extra.IAM.Bindings != nil {
		out = append(out, resource.Resource{
			Type: "gcp.iam", UID: "iam:proj-1", Account: "proj-1",
			Attrs: map[string]any{"bindings": extra.IAM.Bindings},
		})
	}
	return out, nil
}
