package inventory

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

var inventoryBucketName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,253}[A-Za-z0-9]$`)

func awsBuckets(ctx context.Context, r Request, client *http.Client) ([]Resource, error) {
	ec, err := awsClient(r, client, 3)
	if err != nil {
		return nil, err
	}
	svc := s3.New(s3.Options{Region: r.Region, Credentials: ec.Options().Credentials, HTTPClient: client, RetryMaxAttempts: 3, UsePathStyle: true})
	var token *string
	seen := map[string]bool{}
	return infrastructurePages(ctx, r, func(page int) ([]Resource, int, error) {
		result, e := svc.ListBuckets(ctx, &s3.ListBucketsInput{BucketRegion: aws.String(r.Region), MaxBuckets: aws.Int32(100), ContinuationToken: token})
		if e != nil {
			return nil, 0, e
		}
		rows := []Resource{}
		for _, bucket := range result.Buckets {
			name := aws.ToString(bucket.Name)
			if !inventoryBucketName.MatchString(name) || strings.Contains(name, "..") || aws.ToString(bucket.BucketRegion) != r.Region {
				return nil, 0, errors.New("invalid bucket identity or region")
			}
			labels := map[string]string{}
			tags, e := svc.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: bucket.Name})
			if e != nil {
				var api smithy.APIError
				if !errors.As(e, &api) || api.ErrorCode() != "NoSuchTagSet" {
					return nil, 0, e
				}
			} else {
				for _, tag := range tags.TagSet {
					key := aws.ToString(tag.Key)
					if key == "" {
						return nil, 0, errors.New("invalid bucket tag")
					}
					if _, exists := labels[key]; exists {
						return nil, 0, errors.New("duplicate bucket tag")
					}
					labels[key] = aws.ToString(tag.Value)
				}
			}
			size := ""
			if bucket.CreationDate != nil {
				size = "Reported creation: " + bucket.CreationDate.UTC().Format(time.RFC3339)
			}
			rows = append(rows, Resource{NativeID: name, Name: name, Region: r.Region, Status: "present", Size: size, Tags: &Tags{Labels: labels}})
		}
		token = result.ContinuationToken
		if aws.ToString(token) == "" {
			return rows, 0, nil
		}
		if len(*token) > 1024 || seen[*token] {
			return nil, 0, errors.New("invalid bucket pagination")
		}
		seen[*token] = true
		return rows, page + 1, nil
	})
}
