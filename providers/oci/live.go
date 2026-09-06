package oci

import (
	"context"
	"fmt"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

type LiveOpts struct {
	TenancyOCID, UserOCID, Fingerprint, PrivateKey, Passphrase, Region string
	ComputeClient                                                      *core.ComputeClient
}

func DiscoverLive(ctx context.Context, options LiveOpts) ([]resource.Resource, []string, error) {
	if options.TenancyOCID == "" {
		return nil, nil, fmt.Errorf("OCI tenancyOcid required")
	}
	client := options.ComputeClient
	if client == nil {
		if options.UserOCID == "" || options.Fingerprint == "" || options.PrivateKey == "" || options.Region == "" {
			return nil, nil, fmt.Errorf("OCI userOcid, fingerprint, privateKey, and region required")
		}
		var passphrase *string
		if options.Passphrase != "" {
			passphrase = &options.Passphrase
		}
		provider := common.NewRawConfigurationProvider(options.TenancyOCID, options.UserOCID, options.Region, options.Fingerprint, options.PrivateKey, passphrase)
		created, err := core.NewComputeClientWithConfigurationProvider(provider)
		if err != nil {
			return nil, nil, fmt.Errorf("OCI compute client: %w", err)
		}
		client = &created
	}
	if client.RetryPolicy() == nil {
		policy := common.NewRetryPolicy(4, common.DefaultShouldRetryOperation, func(common.OCIOperationResponse) time.Duration {
			return 200 * time.Millisecond
		})
		client.Configuration.RetryPolicy = &policy
	}
	out := []resource.Resource{{Type: "cloud_account", UID: options.TenancyOCID, Account: options.TenancyOCID, Region: options.Region, Aliases: []resource.Alias{{Kind: "oci_tenancy_ocid", Value: options.TenancyOCID}}}}
	var page *string
	for {
		response, err := client.ListInstances(ctx, core.ListInstancesRequest{CompartmentId: &options.TenancyOCID, Page: page, Limit: common.Int(1000)})
		if err != nil {
			return out, []string{"OCI instances: " + err.Error()}, nil
		}
		for _, instance := range response.Items {
			if instance.Id == nil || *instance.Id == "" {
				continue
			}
			uid := *instance.Id
			out = append(out, resource.Resource{Type: "oci.resource", UID: uid, Account: options.TenancyOCID, Region: stringValue(instance.Region),
				Aliases: []resource.Alias{{Kind: "oci_ocid", Value: uid}},
				Attrs:   map[string]any{"name": stringValue(instance.DisplayName), "lifecycle": instance.LifecycleState, "shape": stringValue(instance.Shape), "availability_domain": stringValue(instance.AvailabilityDomain), "compartment_id": stringValue(instance.CompartmentId), "freeform_tags": instance.FreeformTags, "defined_tags": instance.DefinedTags},
				Edges:   []resource.Edge{{From: options.TenancyOCID, To: uid, Kind: "contains"}},
			})
		}
		if response.OpcNextPage == nil || *response.OpcNextPage == "" {
			break
		}
		page = response.OpcNextPage
	}
	// Initial preview scope is explicit. Root-compartment compute inventory is
	// live, while recursive compartments and other services remain partial.
	return out, []string{"OCI preview currently covers compute instances in the tenancy root compartment only"}, nil
}

func stringValue(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return *pointer
}
