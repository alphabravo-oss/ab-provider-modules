package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/protocol"
	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
	"github.com/alphabravo-oss/ab-provider-modules/providers/oci"
)

func main() { protocol.Serve("oci", "2.0.0", discover) }

func discover(ctx context.Context, raw json.RawMessage) ([]resource.Resource, []string, error) {
	var config struct {
		TenancyOCID string `json:"tenancyOcid"`
		UserOCID    string `json:"userOcid"`
		Fingerprint string `json:"fingerprint"`
		PrivateKey  string `json:"privateKey"`
		Passphrase  string `json:"passphrase"`
		Region      string `json:"region"`
	}
	if err := json.Unmarshal(raw, &config); err != nil || config.TenancyOCID == "" || config.UserOCID == "" || config.Fingerprint == "" || config.PrivateKey == "" || config.Region == "" {
		return nil, nil, fmt.Errorf("valid OCI tenancyOcid, userOcid, fingerprint, privateKey, and region required")
	}
	return oci.DiscoverLive(ctx, oci.LiveOpts{TenancyOCID: config.TenancyOCID, UserOCID: config.UserOCID, Fingerprint: config.Fingerprint, PrivateKey: config.PrivateKey, Passphrase: config.Passphrase, Region: config.Region})
}
