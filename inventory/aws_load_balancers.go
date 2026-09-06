package inventory

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
)

func awsLoadBalancers(ctx context.Context, r Request, client *http.Client) ([]Resource, error) {
	ec, err := awsClient(r, client, 3)
	if err != nil {
		return nil, err
	}
	modern := elasticloadbalancingv2.New(elasticloadbalancingv2.Options{Region: r.Region, Credentials: ec.Options().Credentials, HTTPClient: client, RetryMaxAttempts: 3})
	classic := elasticloadbalancing.New(elasticloadbalancing.Options{Region: r.Region, Credentials: ec.Options().Credentials, HTTPClient: client, RetryMaxAttempts: 3})
	all := []Resource{}
	for _, legacy := range []bool{false, true} {
		var marker *string
		seen := map[string]bool{}
		rows, err := infrastructurePages(ctx, r, func(page int) ([]Resource, int, error) {
			rows := []Resource{}
			// Twenty resources fit one DescribeTags call in both API generations.
			if legacy {
				p, e := classic.DescribeLoadBalancers(ctx, &elasticloadbalancing.DescribeLoadBalancersInput{PageSize: aws.Int32(20), Marker: marker})
				if e != nil {
					return nil, 0, e
				}
				marker = p.NextMarker
				for _, v := range p.LoadBalancerDescriptions {
					id := aws.ToString(v.LoadBalancerName)
					if id == "" || aws.ToString(v.Scheme) == "" {
						return nil, 0, errors.New("missing classic load balancer metadata")
					}
					rows = append(rows, Resource{NativeID: id, Name: id, Region: r.Region, Status: "present", Size: fmt.Sprintf("classic · %s · %d zones · %d instances", aws.ToString(v.Scheme), len(v.AvailabilityZones), len(v.Instances))})
				}
			} else {
				p, e := modern.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{PageSize: aws.Int32(20), Marker: marker})
				if e != nil {
					return nil, 0, e
				}
				marker = p.NextMarker
				for _, v := range p.LoadBalancers {
					if aws.ToString(v.LoadBalancerArn) == "" || aws.ToString(v.LoadBalancerName) == "" || v.State == nil || v.State.Code == "" || v.Type == "" {
						return nil, 0, errors.New("missing load balancer metadata")
					}
					rows = append(rows, Resource{NativeID: *v.LoadBalancerArn, Name: *v.LoadBalancerName, Region: r.Region, Status: string(v.State.Code), Size: fmt.Sprintf("%s · %s · %s · %d zones", v.Type, v.Scheme, v.IpAddressType, len(v.AvailabilityZones))})
				}
			}
			if len(rows) > 20 {
				return nil, 0, errors.New("load balancer page limit")
			}
			ids := []string{}
			indices := map[string]int{}
			for i, row := range rows {
				if _, ok := indices[row.NativeID]; ok {
					return nil, 0, errors.New("duplicate load balancer identity")
				}
				ids = append(ids, row.NativeID)
				indices[row.NativeID] = i
			}
			setTags := func(id string, labels map[string]string) error {
				i, ok := indices[id]
				if !ok || rows[i].Tags != nil {
					return errors.New("unexpected load balancer tag identity")
				}
				rows[i].Tags = &Tags{Labels: labels}
				return nil
			}
			if len(ids) > 0 {
				if legacy {
					p, e := classic.DescribeTags(ctx, &elasticloadbalancing.DescribeTagsInput{LoadBalancerNames: ids})
					if e != nil {
						return nil, 0, e
					}
					for _, v := range p.TagDescriptions {
						labels := map[string]string{}
						for _, tag := range v.Tags {
							labels[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
						}
						if e = setTags(aws.ToString(v.LoadBalancerName), labels); e != nil {
							return nil, 0, e
						}
					}
				} else {
					p, e := modern.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{ResourceArns: ids})
					if e != nil {
						return nil, 0, e
					}
					for _, v := range p.TagDescriptions {
						labels := map[string]string{}
						for _, tag := range v.Tags {
							labels[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
						}
						if e = setTags(aws.ToString(v.ResourceArn), labels); e != nil {
							return nil, 0, e
						}
					}
				}
				for _, row := range rows {
					if row.Tags == nil {
						return nil, 0, errors.New("missing load balancer tags")
					}
				}
			}
			if aws.ToString(marker) == "" {
				return rows, 0, nil
			}
			if seen[*marker] {
				return nil, 0, errors.New("repeated load balancer page marker")
			}
			seen[*marker] = true
			return rows, page + 1, nil
		})
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if len(all) > MaxResources {
			return nil, errors.New("resource limit")
		}
	}
	return all, nil
}
