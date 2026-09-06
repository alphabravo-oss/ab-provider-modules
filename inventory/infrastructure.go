package inventory

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/digitalocean/godo"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"golang.org/x/oauth2"
)

// Each SDK fetches one page. A failed page discards the entire connection snapshot.
func infrastructurePages(ctx context.Context, r Request, page func(int) ([]Resource, int, error)) ([]Resource, error) {
	out := []Resource{}
	for n := 1; n <= MaxResources; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, next, err := page(n)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.Region == "" {
				return nil, errors.New("missing resource region")
			}
			if r.Region == "" || row.Region == r.Region || row.Region == "global" {
				out = append(out, row)
			}
		}
		if len(out) > MaxResources {
			return nil, errors.New("resource limit")
		}
		if next == 0 {
			return out, nil
		}
		if next <= n {
			return nil, errors.New("invalid pagination")
		}
		n = next
	}
	return nil, errors.New("page limit")
}

func awsInfrastructure(ctx context.Context, r Request, client *http.Client, kind string) ([]Resource, error) {
	if kind == "storage.bucket" {
		return awsBuckets(ctx, r, client)
	}
	if kind == "kubernetes.cluster" || kind == "kubernetes.node_group" {
		return awsEKS(ctx, r, client, kind)
	}
	if kind == "network.load_balancer" {
		return awsLoadBalancers(ctx, r, client)
	}
	if strings.HasPrefix(kind, "database.") {
		return awsDatabases(ctx, r, client, kind)
	}
	if kind == "dns.zone" || kind == "dns.record" {
		return awsDNS(ctx, r, client, kind)
	}
	svc, err := awsClient(r, client, 3)
	if err != nil {
		return nil, err
	}
	var token *string
	seenTokens := map[string]bool{}
	name := func(id string, tags []types.Tag) string {
		for _, t := range tags {
			if aws.ToString(t.Key) == "Name" {
				return aws.ToString(t.Value)
			}
		}
		return id
	}
	return infrastructurePages(ctx, r, func(n int) ([]Resource, int, error) {
		rows := []Resource{}
		switch kind {
		case "compute.placement_group":
			p, e := svc.DescribePlacementGroups(ctx, &ec2.DescribePlacementGroupsInput{})
			if e != nil {
				return nil, 0, e
			}
			for _, v := range p.PlacementGroups {
				id, groupName := aws.ToString(v.GroupId), aws.ToString(v.GroupName)
				if id == "" || groupName == "" || v.State == "" || v.Strategy == "" || aws.ToInt32(v.PartitionCount) < 0 {
					return nil, 0, errors.New("invalid placement group metadata")
				}
				if v.State == types.PlacementGroupStateDeleted {
					continue
				}
				size := string(v.Strategy)
				if v.PartitionCount != nil {
					size += fmt.Sprintf(" · %d partitions", *v.PartitionCount)
				}
				if v.SpreadLevel != "" {
					size += " · " + string(v.SpreadLevel)
				}
				rows = append(rows, Resource{NativeID: id, Name: groupName, Region: r.Region, Status: string(v.State), Size: size, Tags: ec2Labels(v.Tags)})
			}
		case "network.route_table":
			p, e := svc.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{MaxResults: aws.Int32(100), NextToken: token})
			if e != nil {
				return nil, 0, e
			}
			token = p.NextToken
			for _, v := range p.RouteTables {
				id := aws.ToString(v.RouteTableId)
				if id == "" || aws.ToString(v.VpcId) == "" {
					return nil, 0, errors.New("invalid route table metadata")
				}
				blackholes := 0
				main := false
				for _, route := range v.Routes {
					if route.State == types.RouteStateBlackhole {
						blackholes++
					}
				}
				for _, association := range v.Associations {
					main = main || aws.ToBool(association.Main)
				}
				status := "present"
				if blackholes > 0 {
					status = "degraded"
				}
				rows = append(rows, Resource{Tags: ec2Labels(v.Tags), NativeID: id, Name: name(id, v.Tags), Region: r.Region, Status: status, Size: fmt.Sprintf("%s · %d routes · %d blackholes · %d explicit associations · main=%t", aws.ToString(v.VpcId), len(v.Routes), blackholes, len(v.Associations), main)})
			}
		case "network.internet_gateway":
			p, e := svc.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{MaxResults: aws.Int32(100), NextToken: token})
			if e != nil {
				return nil, 0, e
			}
			token = p.NextToken
			for _, v := range p.InternetGateways {
				id := aws.ToString(v.InternetGatewayId)
				if id == "" {
					return nil, 0, errors.New("invalid internet gateway metadata")
				}
				status, size := "detached", "No VPC attachment"
				if len(v.Attachments) > 0 {
					parts := []string{}
					for _, a := range v.Attachments {
						if aws.ToString(a.VpcId) == "" || a.State == "" {
							return nil, 0, errors.New("invalid gateway attachment")
						}
						parts = append(parts, aws.ToString(a.VpcId)+" ("+string(a.State)+")")
					}
					status, size = "attached", strings.Join(parts, ", ")
				}
				rows = append(rows, Resource{Tags: ec2Labels(v.Tags), NativeID: id, Name: name(id, v.Tags), Region: r.Region, Status: status, Size: size})
			}
		case "network.nat_gateway":
			p, e := svc.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{MaxResults: aws.Int32(100), NextToken: token})
			if e != nil {
				return nil, 0, e
			}
			token = p.NextToken
			for _, v := range p.NatGateways {
				if v.State == types.NatGatewayStateDeleted {
					continue
				}
				id := aws.ToString(v.NatGatewayId)
				if id == "" || aws.ToString(v.VpcId) == "" || v.State == "" {
					return nil, 0, errors.New("invalid NAT gateway metadata")
				}
				rows = append(rows, Resource{Tags: ec2Labels(v.Tags), NativeID: id, Name: name(id, v.Tags), Region: r.Region, Status: string(v.State), Size: fmt.Sprintf("%s · subnet %s · %s · %s · %d addresses", aws.ToString(v.VpcId), aws.ToString(v.SubnetId), v.ConnectivityType, v.AvailabilityMode, len(v.NatGatewayAddresses))})
			}
		case "network.network":
			p, e := svc.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{MaxResults: aws.Int32(1000), NextToken: token})
			if e != nil {
				return nil, 0, e
			}
			token = p.NextToken
			for _, v := range p.Vpcs {
				id := aws.ToString(v.VpcId)
				rows = append(rows, Resource{Tags: ec2Labels(v.Tags), NativeID: id, Name: name(id, v.Tags), Region: r.Region, Status: string(v.State), Size: aws.ToString(v.CidrBlock)})
			}
		case "network.subnet":
			p, e := svc.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{MaxResults: aws.Int32(1000), NextToken: token})
			if e != nil {
				return nil, 0, e
			}
			token = p.NextToken
			for _, v := range p.Subnets {
				id := aws.ToString(v.SubnetId)
				rows = append(rows, Resource{Tags: ec2Labels(v.Tags), NativeID: id, Name: name(id, v.Tags), Region: r.Region, Status: string(v.State), Size: fmt.Sprintf("%s · %s · %s", aws.ToString(v.CidrBlock), aws.ToString(v.VpcId), aws.ToString(v.AvailabilityZone))})
			}
		case "network.ip":
			p, e := svc.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
			if e != nil {
				return nil, 0, e
			}
			for _, v := range p.Addresses {
				id := aws.ToString(v.AllocationId)
				if id == "" || net.ParseIP(aws.ToString(v.PublicIp)) == nil {
					return nil, 0, errors.New("missing allocation metadata")
				}
				status := "unassigned"
				if aws.ToString(v.AssociationId) != "" {
					status = "assigned"
				}
				rows = append(rows, Resource{Tags: ec2Labels(v.Tags), NativeID: id, Name: name(id, v.Tags), Region: r.Region, Status: status, PublicIP: aws.ToString(v.PublicIp), PrivateIP: aws.ToString(v.PrivateIpAddress), Size: aws.ToString(v.NetworkBorderGroup)})
			}
		case "network.firewall":
			p, e := svc.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{MaxResults: aws.Int32(1000), NextToken: token})
			if e != nil {
				return nil, 0, e
			}
			token = p.NextToken
			for _, v := range p.SecurityGroups {
				id := aws.ToString(v.GroupId)
				rows = append(rows, Resource{Tags: ec2Labels(v.Tags), NativeID: id, Name: aws.ToString(v.GroupName), Region: r.Region, Status: "present"})
			}
		case "storage.volume":
			p, e := svc.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{MaxResults: aws.Int32(500), NextToken: token})
			if e != nil {
				return nil, 0, e
			}
			token = p.NextToken
			for _, v := range p.Volumes {
				id := aws.ToString(v.VolumeId)
				rows = append(rows, Resource{Tags: ec2Labels(v.Tags), NativeID: id, Name: name(id, v.Tags), Region: r.Region, Status: string(v.State), Size: strconv.Itoa(int(aws.ToInt32(v.Size))) + " GiB"})
			}
		case "storage.snapshot":
			p, e := svc.DescribeSnapshots(ctx, &ec2.DescribeSnapshotsInput{OwnerIds: []string{"self"}, MaxResults: aws.Int32(1000), NextToken: token})
			if e != nil {
				return nil, 0, e
			}
			token = p.NextToken
			for _, v := range p.Snapshots {
				id := aws.ToString(v.SnapshotId)
				rows = append(rows, Resource{Tags: ec2Labels(v.Tags), NativeID: id, Name: name(id, v.Tags), Region: r.Region, Status: string(v.State), Size: fmt.Sprintf("%d GiB source volume · %s", aws.ToInt32(v.VolumeSize), aws.ToString(v.VolumeId))})
			}
		case "compute.image":
			p, e := svc.DescribeImages(ctx, &ec2.DescribeImagesInput{Owners: []string{"self"}, MaxResults: aws.Int32(1000), NextToken: token})
			if e != nil {
				return nil, 0, e
			}
			token = p.NextToken
			for _, v := range p.Images {
				id := aws.ToString(v.ImageId)
				label := aws.ToString(v.Name)
				if label == "" {
					label = id
				}
				rows = append(rows, Resource{Tags: ec2Labels(v.Tags), NativeID: id, Name: label, Region: r.Region, Status: string(v.State), Size: string(v.Architecture)})
			}
		case "access.ssh_key":
			p, e := svc.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{})
			if e != nil {
				return nil, 0, e
			}
			for _, v := range p.KeyPairs {
				rows = append(rows, Resource{Tags: ec2Labels(v.Tags), NativeID: aws.ToString(v.KeyPairId), Name: aws.ToString(v.KeyName), Region: r.Region, Status: "present", Size: aws.ToString(v.KeyFingerprint)})
			}
		case "compute.type":
			p, e := svc.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{MaxResults: aws.Int32(100), NextToken: token})
			if e != nil {
				return nil, 0, e
			}
			token = p.NextToken
			for _, v := range p.InstanceTypes {
				if v.VCpuInfo == nil || v.MemoryInfo == nil {
					return nil, 0, errors.New("missing instance sizing")
				}
				id := string(v.InstanceType)
				rows = append(rows, Resource{NativeID: id, Name: id, Region: r.Region, Status: "catalog", Size: fmt.Sprintf("%d vCPU · %d MiB RAM", aws.ToInt32(v.VCpuInfo.DefaultVCpus), aws.ToInt64(v.MemoryInfo.SizeInMiB))})
			}
		default:
			return nil, 0, errors.New("unsupported kind")
		}
		next := 0
		if aws.ToString(token) != "" {
			if seenTokens[*token] {
				return nil, 0, errors.New("repeated EC2 pagination token")
			}
			seenTokens[*token] = true
			next = n + 1
		}
		return rows, next, nil
	})
}

func doInfrastructure(ctx context.Context, r Request, client *http.Client, kind string) ([]Resource, error) {
	svc := godo.NewClient(&http.Client{Transport: &oauth2.Transport{Source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: r.Credential}), Base: client.Transport}, Timeout: client.Timeout})
	return infrastructurePages(ctx, r, func(n int) ([]Resource, int, error) {
		opts := &godo.ListOptions{Page: n, PerPage: 200}
		rows := []Resource{}
		var resp *godo.Response
		var err error
		switch kind {
		case "organization.project":
			var values []godo.Project
			values, resp, err = svc.Projects.List(ctx, opts)
			for _, v := range values {
				if v.ID == "" || v.Name == "" {
					return nil, 0, errors.New("invalid project metadata")
				}
				rows = append(rows, Resource{NativeID: v.ID, Name: v.Name, Region: "global", Status: "present", Size: fmt.Sprintf("%s · default=%t", v.Environment, v.IsDefault)})
			}
		case "application.app":
			var values []*godo.App
			values, resp, err = svc.Apps.List(ctx, opts)
			for _, v := range values {
				if v == nil || v.ID == "" || v.Spec == nil || v.Spec.Name == "" || v.Region == nil || v.Region.Slug == "" {
					return nil, 0, errors.New("invalid application metadata")
				}
				status := "not_deployed"
				deployment := v.ActiveDeployment
				if v.PendingDeployment != nil {
					deployment = v.PendingDeployment
				}
				if v.InProgressDeployment != nil {
					deployment = v.InProgressDeployment
				}
				if deployment != nil {
					if deployment.Phase == "" {
						return nil, 0, errors.New("missing deployment phase")
					}
					status = strings.ToLower(string(deployment.Phase))
				}
				// Only counts and identity enter inventory; application specs can contain environment secrets.
				spec := v.Spec
				size := fmt.Sprintf("%d services · %d workers · %d jobs · %d static sites · %d functions · %d databases", len(spec.Services), len(spec.Workers), len(spec.Jobs), len(spec.StaticSites), len(spec.Functions), len(spec.Databases))
				rows = append(rows, Resource{NativeID: v.ID, Name: spec.Name, Region: v.Region.Slug, Status: status, Size: size})
			}

		// ponytail: bounded connection scan; split per-zone jobs if DNS accounts exceed the worker deadline.
		case "dns.zone", "dns.record":
			var zones []godo.Domain
			zones, resp, err = svc.Domains.List(ctx, opts)
			for _, zone := range zones {
				if !dnsZoneName(zone.Name) || zone.TTL < 0 {
					return nil, 0, errors.New("invalid DNS zone")
				}
				if kind == "dns.zone" {
					rows = append(rows, Resource{NativeID: zone.Name, Name: zone.Name, Region: "global", Status: "present", Size: fmt.Sprintf("TTL %d seconds", zone.TTL)})
					continue
				}
				records, e := infrastructurePages(ctx, r, func(page int) ([]Resource, int, error) {
					values, response, e := svc.Domains.Records(ctx, zone.Name, &godo.ListOptions{Page: page, PerPage: 200})
					if e != nil {
						return nil, 0, e
					}
					items := []Resource{}
					for _, v := range values {
						if v.ID <= 0 || v.Type == "" || v.Name == "" || v.TTL < 0 {
							return nil, 0, errors.New("invalid DNS record")
						}
						items = append(items, Resource{NativeID: zone.Name + "/" + strconv.Itoa(v.ID), Name: v.Name + " · " + zone.Name, Region: "global", Status: "present", Size: fmt.Sprintf("%s · TTL %d seconds", v.Type, v.TTL)})
					}
					next, e := doNextPage(response, page)
					return items, next, e
				})
				if e != nil {
					return nil, 0, e
				}
				if len(rows)+len(records) > MaxResources {
					return nil, 0, errors.New("resource limit")
				}
				rows = append(rows, records...)
			}
		case "database.cluster":
			var values []godo.Database
			values, resp, err = svc.Databases.List(ctx, opts)
			for _, v := range values {
				if v.EngineSlug == "" || v.NumNodes < 0 {
					return nil, 0, errors.New("missing database metadata")
				}
				// Never copy connection URIs, passwords, users or credential-bearing SDK structs.
				rows = append(rows, Resource{Tags: &Tags{Names: v.Tags}, NativeID: v.ID, Name: v.Name, Region: v.RegionSlug, Status: v.Status, Size: fmt.Sprintf("%s %s · %s · %d nodes", v.EngineSlug, v.VersionSlug, v.SizeSlug, v.NumNodes)})
			}
		case "kubernetes.cluster", "kubernetes.node_group":
			var values []*godo.KubernetesCluster
			values, resp, err = svc.Kubernetes.List(ctx, opts)
			for _, v := range values {
				if v == nil || v.Status == nil || v.Status.State == "" {
					return nil, 0, errors.New("missing cluster status")
				}

				if kind == "kubernetes.node_group" {
					if v.ID == "" || strings.ContainsAny(v.ID, "/?#") || v.Name == "" || v.RegionSlug == "" {
						return nil, 0, errors.New("invalid cluster identity")
					}
					pools, e := infrastructurePages(ctx, r, func(page int) ([]Resource, int, error) {
						// godo ListNodePools drops root.Links in v1.206.0; retain it through the SDK transport.
						request, e := svc.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/v2/kubernetes/clusters/%s/node_pools?page=%d&per_page=200", v.ID, page), nil)
						if e != nil {
							return nil, 0, e
						}
						var root struct {
							NodePools []*godo.KubernetesNodePool `json:"node_pools"`
							Links     *godo.Links                `json:"links"`
						}
						response, e := svc.Do(ctx, request, &root)
						if response != nil {
							response.Links = root.Links
						}
						values := root.NodePools
						if e != nil {
							return nil, 0, e
						}
						items := []Resource{}
						for _, pool := range values {
							if pool == nil || pool.ID == "" || pool.Name == "" || pool.Size == "" || pool.Count < 0 || pool.MinNodes < 0 || pool.MaxNodes < 0 || (pool.AutoScale && pool.MinNodes > pool.MaxNodes) {
								return nil, 0, errors.New("invalid node pool metadata")
							}
							items = append(items, Resource{
								NativeID: pool.ID, Name: v.Name + " / " + pool.Name, Region: v.RegionSlug, Status: "present",
								Tags: &Tags{Names: pool.Tags, Labels: pool.Labels},
								Size: fmt.Sprintf("%s · %d nodes · autoscale %t · min %d / max %d", pool.Size, pool.Count, pool.AutoScale, pool.MinNodes, pool.MaxNodes),
							})
						}
						next, e := doNextPage(response, page)
						return items, next, e
					})
					if e != nil {
						return nil, 0, e
					}
					if len(rows)+len(pools) > MaxResources {
						return nil, 0, errors.New("resource limit")
					}
					rows = append(rows, pools...)
					continue
				}
				rows = append(rows, Resource{Tags: &Tags{Names: v.Tags}, NativeID: v.ID, Name: v.Name, Region: v.RegionSlug, Status: string(v.Status.State), PublicIP: v.IPv4, Size: fmt.Sprintf("%s · %d node pools · HA %t", v.VersionSlug, len(v.NodePools), v.HA)})
			}
		case "network.certificate":
			var values []godo.Certificate
			values, resp, err = svc.Certificates.List(ctx, opts)
			for _, v := range values {
				var expires time.Time
				if v.NotAfter != "" {
					var e error
					expires, e = time.Parse(time.RFC3339, v.NotAfter)
					if e != nil {
						return nil, 0, errors.New("invalid certificate expiry")
					}
				}
				status := v.State
				if status == "" {
					status = "present"
				}
				rows = append(rows, Resource{NativeID: v.ID, Name: v.Name, Region: "global", Status: status, Size: certificateSummary(v.Type, len(v.DNSNames), expires)})
			}
		case "network.network":
			var values []*godo.VPC
			values, resp, err = svc.VPCs.List(ctx, opts)
			for _, v := range values {
				rows = append(rows, Resource{NativeID: v.ID, Name: v.Name, Region: v.RegionSlug, Status: "present", Size: v.IPRange})
			}

		case "network.reserved_ipv6":
			var values []godo.ReservedIPV6
			values, resp, err = svc.ReservedIPV6s.List(ctx, opts)
			for _, v := range values {
				ip := net.ParseIP(v.IP)
				if ip == nil || ip.To4() != nil || v.RegionSlug == "" {
					return nil, 0, errors.New("invalid reserved IPv6 metadata")
				}
				status := "unassigned"
				if v.Droplet != nil {
					if v.Droplet.ID <= 0 {
						return nil, 0, errors.New("invalid IPv6 assignment")
					}
					status = "assigned"
				}
				address := ip.String()
				rows = append(rows, Resource{NativeID: address, Name: address, Region: v.RegionSlug, Status: status, PublicIP: address})
			}
		case "network.ip":
			var values []godo.ReservedIP
			values, resp, err = svc.ReservedIPs.List(ctx, opts)
			for _, v := range values {
				if v.Region == nil || net.ParseIP(v.IP) == nil {
					return nil, 0, errors.New("missing IP metadata")
				}
				status := "unassigned"
				if v.Droplet != nil {
					status = "assigned"
				}
				if v.Locked {
					status = "locked"
				}
				rows = append(rows, Resource{NativeID: v.IP, Name: v.IP, Region: v.Region.Slug, Status: status, PublicIP: v.IP})
			}
		case "network.firewall":
			var values []godo.Firewall
			values, resp, err = svc.Firewalls.List(ctx, opts)
			for _, v := range values {
				rows = append(rows, Resource{NativeID: v.ID, Name: v.Name, Region: "global", Status: v.Status})
			}
		case "storage.volume":
			var values []godo.Volume
			values, resp, err = svc.Storage.ListVolumes(ctx, &godo.ListVolumeParams{ListOptions: opts, Region: r.Region})
			for _, v := range values {
				region := ""
				if v.Region != nil {
					region = v.Region.Slug
				}
				status := "available"
				if len(v.DropletIDs) > 0 {
					status = "attached"
				}
				rows = append(rows, Resource{Tags: &Tags{Names: v.Tags}, NativeID: v.ID, Name: v.Name, Region: region, Status: status, Size: strconv.FormatInt(v.SizeGigaBytes, 10) + " GiB"})
			}
		case "network.load_balancer":
			var values []godo.LoadBalancer
			values, resp, err = svc.LoadBalancers.List(ctx, opts)
			for _, v := range values {
				region := ""
				if v.Region != nil {
					region = v.Region.Slug
				} else if v.Type == "GLOBAL" {
					region = "global"
				}
				rows = append(rows, Resource{NativeID: v.ID, Name: v.Name, Region: region, Status: v.Status, PublicIP: v.IP, Size: v.SizeSlug})
			}
		case "storage.snapshot":
			var values []godo.Snapshot
			values, resp, err = svc.Snapshots.List(ctx, opts)
			for _, v := range values {
				if len(v.Regions) == 0 || (v.ResourceType != "droplet" && v.ResourceType != "volume") || v.ResourceID == "" {
					return nil, 0, errors.New("incomplete snapshot metadata")
				}
				for _, region := range v.Regions {
					rows = append(rows, Resource{NativeID: v.ID, Name: v.Name, Region: region, Status: "present", Size: fmt.Sprintf("%.2f GiB snapshot · %s %s · minimum disk %d GiB", v.SizeGigaBytes, v.ResourceType, v.ResourceID, v.MinDiskSize)})
				}
			}
		case "compute.image", "storage.backup":
			var values []godo.Image
			if kind == "storage.backup" {
				values, resp, err = svc.Images.ListUser(ctx, opts)
			} else {
				values, resp, err = svc.Images.List(ctx, opts)
			}
			for _, v := range values {
				if kind == "storage.backup" && v.Type == "" {
					return nil, 0, errors.New("missing backup image type")
				}
				if kind == "storage.backup" && (v.Type != "backup" || v.Public) {
					continue
				}
				regions := v.Regions
				if len(regions) == 0 {
					regions = []string{"global"}
				}
				for _, region := range regions {
					rows = append(rows, Resource{Catalog: v.Public, Tags: &Tags{Names: v.Tags}, NativeID: strconv.Itoa(v.ID), Name: v.Name, Region: region, Status: v.Status, Size: fmt.Sprintf("%s · minimum disk %d GiB", v.Distribution, v.MinDiskSize)})
				}
			}
		case "access.ssh_key":
			var values []godo.Key
			values, resp, err = svc.Keys.List(ctx, opts)
			for _, v := range values {
				rows = append(rows, Resource{NativeID: strconv.Itoa(v.ID), Name: v.Name, Region: "global", Status: "present", Size: v.Fingerprint})
			}
		case "compute.type":
			var values []godo.Size
			values, resp, err = svc.Sizes.List(ctx, opts)
			for _, v := range values {
				regions := v.Regions
				if len(regions) == 0 {
					regions = []string{"global"}
				}
				status := "unavailable"
				if v.Available {
					status = "catalog"
				}
				for _, region := range regions {
					rows = append(rows, Resource{NativeID: v.Slug, Name: v.Slug, Region: region, Status: status, Size: fmt.Sprintf("%d vCPU · %d MiB RAM · %d GiB disk", v.Vcpus, v.Memory, v.Disk)})
				}
			}
		default:
			return nil, 0, errors.New("unsupported kind")
		}
		if err != nil {
			return nil, 0, err
		}
		next, e := doNextPage(resp, n)
		return rows, next, e
	})
}

func hetznerInfrastructure(ctx context.Context, r Request, client *http.Client, kind string) ([]Resource, error) {
	svc := hcloud.NewClient(hcloud.WithToken(r.Credential), hcloud.WithHTTPClient(client))
	return infrastructurePages(ctx, r, func(n int) ([]Resource, int, error) {
		opts := hcloud.ListOpts{Page: n, PerPage: 50}
		rows := []Resource{}
		var resp *hcloud.Response
		var err error
		switch kind {
		// ponytail: bounded connection scan; split per-zone jobs if DNS accounts exceed the worker deadline.
		case "dns.zone", "dns.record":
			var zones []*hcloud.Zone
			zones, resp, err = svc.Zone.List(ctx, hcloud.ZoneListOpts{ListOpts: opts})
			for _, zone := range zones {
				if zone == nil || zone.ID <= 0 || !dnsZoneName(zone.Name) || zone.TTL < 0 {
					return nil, 0, errors.New("invalid DNS zone")
				}
				id := strconv.FormatInt(zone.ID, 10)
				if kind == "dns.zone" {
					rows = append(rows, Resource{Tags: &Tags{Labels: zone.Labels}, NativeID: id, Name: zone.Name, Region: "global", Status: string(zone.Status), Size: fmt.Sprintf("%s · TTL %d seconds · %d records", zone.Mode, zone.TTL, zone.RecordCount)})
					continue
				}
				records, e := infrastructurePages(ctx, r, func(page int) ([]Resource, int, error) {
					values, response, e := svc.Zone.ListRRSets(ctx, zone, hcloud.ZoneRRSetListOpts{ListOpts: hcloud.ListOpts{Page: page, PerPage: 100}})
					if e != nil {
						return nil, 0, e
					}
					items := []Resource{}
					for _, v := range values {
						if v == nil || v.Name == "" || v.Type == "" {
							return nil, 0, errors.New("invalid DNS record set")
						}
						ttl := zone.TTL
						if v.TTL != nil {
							ttl = *v.TTL
						}
						if ttl < 0 {
							return nil, 0, errors.New("invalid DNS TTL")
						}
						items = append(items, Resource{Tags: &Tags{Labels: v.Labels}, NativeID: id + "/" + v.Name + "/" + string(v.Type), Name: v.Name + " · " + zone.Name, Region: "global", Status: "present", Size: fmt.Sprintf("%s · TTL %d seconds · %d values", v.Type, ttl, len(v.Records))})
					}
					if response == nil || response.Meta.Pagination == nil {
						return nil, 0, errors.New("missing pagination")
					}
					return items, response.Meta.Pagination.NextPage, nil
				})
				if e != nil {
					return nil, 0, e
				}
				if len(rows)+len(records) > MaxResources {
					return nil, 0, errors.New("resource limit")
				}
				rows = append(rows, records...)
			}
		case "network.certificate":
			var values []*hcloud.Certificate
			values, resp, err = svc.Certificate.List(ctx, hcloud.CertificateListOpts{ListOpts: opts})
			for _, v := range values {
				if v == nil || v.ID <= 0 {
					return nil, 0, errors.New("missing certificate identity")
				}
				status := "present"
				if v.Type == hcloud.CertificateTypeManaged {
					status = "unknown"
					if v.Status != nil {
						status = string(v.Status.Issuance)
						if v.Status.IsFailed() {
							status = "failed"
						}
					}
				}
				rows = append(rows, Resource{Tags: &Tags{Labels: v.Labels}, NativeID: strconv.FormatInt(v.ID, 10), Name: v.Name, Region: "global", Status: status, Size: certificateSummary(string(v.Type), len(v.DomainNames), v.NotValidAfter)})
			}
		case "network.network":
			var values []*hcloud.Network
			values, resp, err = svc.Network.List(ctx, hcloud.NetworkListOpts{ListOpts: opts})
			for _, v := range values {
				size := ""
				if v.IPRange != nil {
					size = v.IPRange.String()
				}
				rows = append(rows, Resource{Tags: &Tags{Labels: v.Labels}, NativeID: strconv.FormatInt(v.ID, 10), Name: v.Name, Region: "global", Status: "present", Size: size})
			}
		case "network.primary_ip":
			var values []*hcloud.PrimaryIP
			values, resp, err = svc.PrimaryIP.List(ctx, hcloud.PrimaryIPListOpts{ListOpts: opts})
			for _, v := range values {
				if v.ID <= 0 || v.Location == nil || v.IP == nil {
					return nil, 0, errors.New("missing primary IP metadata")
				}
				status := "unassigned"
				if v.AssigneeID != 0 {
					status = "assigned"
				}
				if v.Blocked {
					status = "blocked"
				}
				size := string(v.Type)
				if v.Network != nil {
					size = v.Network.String()
				}
				rows = append(rows, Resource{Tags: &Tags{Labels: v.Labels}, NativeID: strconv.FormatInt(v.ID, 10), Name: v.Name, Region: v.Location.Name, Status: status, PublicIP: v.IP.String(), Size: size})
			}
		case "network.floating_ip":
			var values []*hcloud.FloatingIP
			values, resp, err = svc.FloatingIP.List(ctx, hcloud.FloatingIPListOpts{ListOpts: opts})
			for _, v := range values {
				if v.ID <= 0 || v.HomeLocation == nil || v.IP == nil {
					return nil, 0, errors.New("missing floating IP metadata")
				}
				status := "unassigned"
				if v.Server != nil {
					status = "assigned"
				}
				if v.Blocked {
					status = "blocked"
				}
				size := string(v.Type)
				if v.Network != nil {
					size = v.Network.String()
				}
				rows = append(rows, Resource{Tags: &Tags{Labels: v.Labels}, NativeID: strconv.FormatInt(v.ID, 10), Name: v.Name, Region: v.HomeLocation.Name, Status: status, PublicIP: v.IP.String(), Size: size})
			}
		case "compute.placement_group":
			var values []*hcloud.PlacementGroup
			values, resp, err = svc.PlacementGroup.List(ctx, hcloud.PlacementGroupListOpts{ListOpts: opts})
			for _, v := range values {
				if v.ID <= 0 || v.Type == "" {
					return nil, 0, errors.New("missing placement group metadata")
				}
				rows = append(rows, Resource{Tags: &Tags{Labels: v.Labels}, NativeID: strconv.FormatInt(v.ID, 10), Name: v.Name, Region: "global", Status: "present", Size: fmt.Sprintf("%s · %d servers", v.Type, len(v.Servers))})
			}
		case "network.firewall":
			var values []*hcloud.Firewall
			values, resp, err = svc.Firewall.List(ctx, hcloud.FirewallListOpts{ListOpts: opts})
			for _, v := range values {
				rows = append(rows, Resource{Tags: &Tags{Labels: v.Labels}, NativeID: strconv.FormatInt(v.ID, 10), Name: v.Name, Region: "global", Status: "present"})
			}
		case "storage.volume":
			var values []*hcloud.Volume
			values, resp, err = svc.Volume.List(ctx, hcloud.VolumeListOpts{ListOpts: opts})
			for _, v := range values {
				region := ""
				if v.Location != nil {
					region = v.Location.Name
				}
				status := string(v.Status)
				if v.Server != nil {
					status = "attached"
				}
				rows = append(rows, Resource{Tags: &Tags{Labels: v.Labels}, NativeID: strconv.FormatInt(v.ID, 10), Name: v.Name, Region: region, Status: status, Size: strconv.Itoa(v.Size) + " GiB"})
			}
		case "network.load_balancer":
			var values []*hcloud.LoadBalancer
			values, resp, err = svc.LoadBalancer.List(ctx, hcloud.LoadBalancerListOpts{ListOpts: opts})
			for _, v := range values {
				region, size, ip := "", "", ""
				if v.Location != nil {
					region = v.Location.Name
				}
				if v.LoadBalancerType != nil {
					size = v.LoadBalancerType.Name
				}
				if v.PublicNet.IPv4.IP != nil {
					ip = v.PublicNet.IPv4.IP.String()
				}
				rows = append(rows, Resource{Tags: &Tags{Labels: v.Labels}, NativeID: strconv.FormatInt(v.ID, 10), Name: v.Name, Region: region, Status: "present", PublicIP: ip, Size: size})
			}
		case "compute.image", "storage.snapshot", "storage.backup":
			var values []*hcloud.Image
			imageOpts := hcloud.ImageListOpts{ListOpts: opts}
			if kind == "storage.snapshot" {
				imageOpts.Type = []hcloud.ImageType{hcloud.ImageTypeSnapshot}
			}
			if kind == "storage.backup" {
				imageOpts.Type = []hcloud.ImageType{hcloud.ImageTypeBackup}
			}
			values, resp, err = svc.Image.List(ctx, imageOpts)
			for _, v := range values {
				if len(imageOpts.Type) > 0 && v.Type == "" {
					return nil, 0, errors.New("missing recovery image type")
				}
				if v.IsDeleted() || (len(imageOpts.Type) > 0 && v.Type != imageOpts.Type[0]) {
					continue
				}
				name := v.Name
				if name == "" {
					name = v.Description
				}
				rows = append(rows, Resource{Catalog: v.Type == hcloud.ImageTypeSystem, Tags: &Tags{Labels: v.Labels}, NativeID: strconv.FormatInt(v.ID, 10), Name: name, Region: "global", Status: string(v.Status), Size: fmt.Sprintf("%s · %s · %s %s", v.Type, v.Architecture, v.OSFlavor, v.OSVersion)})
			}
		case "access.ssh_key":
			var values []*hcloud.SSHKey
			values, resp, err = svc.SSHKey.List(ctx, hcloud.SSHKeyListOpts{ListOpts: opts})
			for _, v := range values {
				rows = append(rows, Resource{Tags: &Tags{Labels: v.Labels}, NativeID: strconv.FormatInt(v.ID, 10), Name: v.Name, Region: "global", Status: "present", Size: v.Fingerprint})
			}
		case "compute.type":
			var values []*hcloud.ServerType
			values, resp, err = svc.ServerType.List(ctx, hcloud.ServerTypeListOpts{ListOpts: opts})
			for _, v := range values {
				locations := v.Locations
				if len(locations) == 0 {
					locations = []hcloud.ServerTypeLocation{{Location: &hcloud.Location{Name: "global"}}}
				}
				for _, location := range locations {
					if location.Location == nil {
						return nil, 0, errors.New("missing type location")
					}
					status := "unavailable"
					if location.Available || location.Location.Name == "global" {
						status = "catalog"
					}
					rows = append(rows, Resource{NativeID: strconv.FormatInt(v.ID, 10), Name: v.Name, Region: location.Location.Name, Status: status, Size: fmt.Sprintf("%d vCPU · %.1f GiB RAM · %d GiB disk · %s", v.Cores, v.Memory, v.Disk, v.Architecture)})
				}
			}
		default:
			return nil, 0, errors.New("unsupported kind")
		}
		if err != nil {
			return nil, 0, err
		}
		if resp == nil || resp.Meta.Pagination == nil {
			return nil, 0, errors.New("missing pagination")
		}
		return rows, resp.Meta.Pagination.NextPage, nil
	})
}

func certificateSummary(kind string, domains int, expires time.Time) string {
	expiry := "not reported"
	if !expires.IsZero() {
		expiry = expires.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("%s · %d domains · expires %s", kind, domains, expiry)
}

// Only API-returned DNS zone names enter the SDK's domain path.
func dnsZoneName(name string) bool {
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func doNextPage(resp *godo.Response, n int) (int, error) {
	if resp == nil {
		return 0, errors.New("missing response")
	}
	if resp.Links == nil || resp.Links.IsLastPage() {
		return 0, nil
	}
	current, err := resp.Links.CurrentPage()
	if err != nil {
		return 0, err
	}
	if current < n {
		return 0, errors.New("invalid pagination")
	}
	return current + 1, nil
}

func ec2Labels(tags []types.Tag) *Tags {
	labels := map[string]string{}
	for _, tag := range tags {
		labels[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return &Tags{Labels: labels}
}
