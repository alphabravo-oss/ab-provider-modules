package inventory

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	sharedhetzner "github.com/alphabravo-oss/ab-provider-modules/providers/hetzner"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/digitalocean/godo"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"golang.org/x/oauth2"
)

func awsServers(ctx context.Context, r Request, client *http.Client) ([]Resource, error) {
	svc, err := awsClient(r, client, 3)
	if err != nil {
		return nil, err
	}
	pages := ec2.NewDescribeInstancesPaginator(svc, &ec2.DescribeInstancesInput{MaxResults: aws.Int32(1000)})
	out := []Resource{}
	for pages.HasMorePages() {
		p, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, reservation := range p.Reservations {
			for _, v := range reservation.Instances {
				status := "unknown"
				if v.State != nil {
					status = string(v.State.Name)
				}
				if status == "terminated" {
					continue
				}
				name := aws.ToString(v.InstanceId)
				tags := ec2Labels(v.Tags)
				if label, ok := tags.Labels["Name"]; ok {
					name = label
				}

				out = append(out, Resource{Tags: tags, NativeID: aws.ToString(v.InstanceId), Name: name, Region: r.Region, Status: status, PublicIP: aws.ToString(v.PublicIpAddress), PrivateIP: aws.ToString(v.PrivateIpAddress), Size: string(v.InstanceType)})
			}
		}
		if len(out) > MaxResources {
			return nil, errors.New("resource limit")
		}
	}
	return out, nil
}
func doServers(ctx context.Context, r Request, client *http.Client) ([]Resource, error) {
	auth := &http.Client{Transport: &oauth2.Transport{Source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: r.Credential}), Base: client.Transport}, Timeout: client.Timeout}
	svc := godo.NewClient(auth)
	opts := &godo.ListOptions{Page: 1, PerPage: 200}
	out := []Resource{}
	for {
		rows, resp, err := svc.Droplets.List(ctx, opts)
		if err != nil {
			return nil, err
		}
		for _, v := range rows {
			if v.Region == nil {
				return nil, errors.New("missing resource region")
			}
			if r.Region != "" && v.Region.Slug != r.Region {
				continue
			}
			public, _ := v.PublicIPv4()
			private, _ := v.PrivateIPv4()
			out = append(out, Resource{Tags: &Tags{Names: v.Tags}, NativeID: strconv.Itoa(v.ID), Name: v.Name, Region: v.Region.Slug, Status: v.Status, PublicIP: public, PrivateIP: private, Size: v.SizeSlug})
		}
		if len(out) > MaxResources {
			return nil, errors.New("resource limit")
		}
		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}
		next, err := resp.Links.CurrentPage()
		if err != nil {
			return nil, err
		}
		if next < opts.Page {
			return nil, errors.New("invalid pagination")
		}
		opts.Page = next + 1
	}
	return out, nil
}
func hetznerServers(ctx context.Context, r Request, client *http.Client) ([]Resource, error) {
	svc := hcloud.NewClient(hcloud.WithToken(r.Credential), hcloud.WithHTTPClient(client))
	rows, err := sharedhetzner.ListServers(ctx, svc, MaxResources)
	if err != nil {
		return nil, err
	}
	out := []Resource{}
	for _, v := range rows {
		location := ""
		if v.Location != nil {
			location = v.Location.Name
		}
		if r.Region != "" && location != r.Region {
			continue
		}
		public, private, size := "", "", ""
		if v.PublicNet.IPv4.IP != nil {
			public = v.PublicNet.IPv4.IP.String()
		}
		if len(v.PrivateNet) > 0 {
			private = v.PrivateNet[0].IP.String()
		}
		if v.ServerType != nil {
			size = v.ServerType.Name
		}
		out = append(out, Resource{Tags: &Tags{Labels: v.Labels}, NativeID: strconv.FormatInt(v.ID, 10), Name: v.Name, Region: location, Status: string(v.Status), PublicIP: public, PrivateIP: private, Size: size})
	}
	return out, nil
}
