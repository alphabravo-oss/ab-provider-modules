package inventory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
)

var eksName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`)

func eksNames(ctx context.Context, page func(*string) ([]string, *string, error)) ([]string, error) {
	names := []string{}
	seenNames := map[string]bool{}
	seenTokens := map[string]bool{}
	var token *string
	for n := 0; n < 1000; n++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		values, next, err := page(token)
		if err != nil {
			return nil, err
		}
		for _, name := range values {
			if !eksName.MatchString(name) || seenNames[name] {
				return nil, errors.New("invalid or duplicate EKS name")
			}
			seenNames[name] = true
			names = append(names, name)
		}
		if len(names) > MaxResources {
			return nil, errors.New("EKS resource limit")
		}
		if aws.ToString(next) == "" {
			return names, nil
		}
		if seenTokens[*next] {
			return nil, errors.New("repeated EKS page token")
		}
		seenTokens[*next] = true
		token = next
	}
	return nil, errors.New("EKS page limit")
}
func awsEKS(ctx context.Context, r Request, client *http.Client, kind string) ([]Resource, error) {
	ec, err := awsClient(r, client, 3)
	if err != nil {
		return nil, err
	}
	svc := eks.New(eks.Options{Region: r.Region, Credentials: ec.Options().Credentials, HTTPClient: client, RetryMaxAttempts: 3})
	clusters, err := eksNames(ctx, func(token *string) ([]string, *string, error) {
		p, e := svc.ListClusters(ctx, &eks.ListClustersInput{MaxResults: aws.Int32(100), NextToken: token})
		if e != nil {
			return nil, nil, e
		}
		return p.Clusters, p.NextToken, nil
	})
	if err != nil {
		return nil, err
	}
	out := []Resource{}
	for _, cluster := range clusters {
		if kind == "kubernetes.cluster" {
			p, e := svc.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(cluster)})
			if e != nil {
				return nil, e
			}
			v := p.Cluster
			if v == nil || aws.ToString(v.Name) != cluster || v.Status == "" {
				return nil, errors.New("missing EKS cluster metadata")
			}
			out = append(out, Resource{NativeID: cluster, Name: cluster, Region: r.Region, Status: strings.ToLower(string(v.Status)), Size: strings.TrimSpace("Kubernetes " + aws.ToString(v.Version) + " · " + aws.ToString(v.PlatformVersion)), Tags: &Tags{Labels: v.Tags}})
		} else {
			groups, e := eksNames(ctx, func(token *string) ([]string, *string, error) {
				p, e := svc.ListNodegroups(ctx, &eks.ListNodegroupsInput{ClusterName: aws.String(cluster), MaxResults: aws.Int32(100), NextToken: token})
				if e != nil {
					return nil, nil, e
				}
				return p.Nodegroups, p.NextToken, nil
			})
			if e != nil {
				return nil, e
			}
			for _, name := range groups {
				if len(name) > 63 {
					return nil, errors.New("invalid EKS node group name")
				}
				p, e := svc.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{ClusterName: aws.String(cluster), NodegroupName: aws.String(name)})
				if e != nil {
					return nil, e
				}
				v := p.Nodegroup
				if v == nil || aws.ToString(v.NodegroupName) != name || aws.ToString(v.ClusterName) != cluster || v.Status == "" {
					return nil, errors.New("missing EKS node group metadata")
				}
				size := "Kubernetes " + aws.ToString(v.Version) + " · " + string(v.CapacityType)
				if v.ScalingConfig != nil {
					for _, field := range []struct {
						name  string
						value *int32
					}{{"desired", v.ScalingConfig.DesiredSize}, {"min", v.ScalingConfig.MinSize}, {"max", v.ScalingConfig.MaxSize}} {
						if field.value != nil {
							if *field.value < 0 {
								return nil, errors.New("invalid EKS capacity")
							}
							size += fmt.Sprintf(" · %s=%d", field.name, *field.value)
						}
					}
				}
				out = append(out, Resource{NativeID: cluster + ":" + name, Name: cluster + " / " + name, Region: r.Region, Status: strings.ToLower(string(v.Status)), Size: size, Tags: &Tags{Labels: v.Tags}})
				if len(out) > MaxResources {
					return nil, errors.New("EKS resource limit")
				}
			}
		}
	}
	return out, nil
}
