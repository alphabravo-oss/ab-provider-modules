package inventory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

func awsDatabases(ctx context.Context, request Request, client *http.Client, kind string) ([]Resource, error) {
	ec, err := awsClient(request, client, 3)
	if err != nil {
		return nil, err
	}
	svc := rds.New(rds.Options{Region: request.Region, Credentials: ec.Options().Credentials, HTTPClient: client, RetryMaxAttempts: 3})
	var marker *string
	seen := map[string]bool{}
	return infrastructurePages(ctx, request, func(page int) ([]Resource, int, error) {
		rows := []Resource{}
		// Copy only display metadata. SDK objects also contain endpoints, usernames and secret references.
		add := func(id, status, engine, version, class *string, storage *int32, identity *string, tags []rdstypes.Tag) error {
			if aws.ToString(id) == "" || aws.ToString(status) == "" || aws.ToString(engine) == "" {
				return errors.New("missing database metadata")
			}
			parts := []string{strings.TrimSpace(aws.ToString(engine) + " " + aws.ToString(version))}
			if aws.ToString(class) != "" {
				parts = append(parts, *class)
			}
			if storage != nil {
				if *storage < 0 {
					return errors.New("invalid database storage")
				}
				parts = append(parts, fmt.Sprintf("%d GiB allocated", *storage))
			}
			labels := map[string]string{}
			for _, tag := range tags {
				labels[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
			}
			rows = append(rows, Resource{Tags: &Tags{Labels: labels}, ProviderIdentity: aws.ToString(identity), NativeID: *id, Name: *id, Region: request.Region, Status: *status, Size: strings.Join(parts, " · ")})
			return nil
		}
		switch kind {
		case "database.instance":
			p, e := svc.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{MaxRecords: aws.Int32(100), Marker: marker})
			if e != nil {
				return nil, 0, e
			}
			marker = p.Marker
			for _, v := range p.DBInstances {
				if e = add(v.DBInstanceIdentifier, v.DBInstanceStatus, v.Engine, v.EngineVersion, v.DBInstanceClass, v.AllocatedStorage, v.DbiResourceId, v.TagList); e != nil {
					return nil, 0, e
				}
			}
		case "database.cluster":
			p, e := svc.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{MaxRecords: aws.Int32(100), Marker: marker})
			if e != nil {
				return nil, 0, e
			}
			marker = p.Marker
			for _, v := range p.DBClusters {
				if e = add(v.DBClusterIdentifier, v.Status, v.Engine, v.EngineVersion, v.EngineMode, v.AllocatedStorage, v.DbClusterResourceId, v.TagList); e != nil {
					return nil, 0, e
				}
			}
		case "database.snapshot":
			p, e := svc.DescribeDBSnapshots(ctx, &rds.DescribeDBSnapshotsInput{MaxRecords: aws.Int32(100), Marker: marker, IncludePublic: aws.Bool(false), IncludeShared: aws.Bool(false)})
			if e != nil {
				return nil, 0, e
			}
			marker = p.Marker
			for _, v := range p.DBSnapshots {
				if e = add(v.DBSnapshotIdentifier, v.Status, v.Engine, v.EngineVersion, v.SnapshotType, v.AllocatedStorage, nil, v.TagList); e != nil {
					return nil, 0, e
				}
			}
		case "database.cluster_snapshot":
			p, e := svc.DescribeDBClusterSnapshots(ctx, &rds.DescribeDBClusterSnapshotsInput{MaxRecords: aws.Int32(100), Marker: marker, IncludePublic: aws.Bool(false), IncludeShared: aws.Bool(false)})
			if e != nil {
				return nil, 0, e
			}
			marker = p.Marker
			for _, v := range p.DBClusterSnapshots {
				if e = add(v.DBClusterSnapshotIdentifier, v.Status, v.Engine, v.EngineVersion, v.SnapshotType, v.AllocatedStorage, nil, v.TagList); e != nil {
					return nil, 0, e
				}
			}
		default:
			return nil, 0, errors.New("unsupported database kind")
		}
		token := aws.ToString(marker)
		if token == "" {
			return rows, 0, nil
		}
		if seen[token] {
			return nil, 0, errors.New("repeated database page marker")
		}
		seen[token] = true
		return rows, page + 1, nil
	})
}
