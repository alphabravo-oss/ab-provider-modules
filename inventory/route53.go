package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
)

func awsDNS(ctx context.Context, r Request, client *http.Client, kind string) ([]Resource, error) {
	ec, err := awsClient(r, client, 3)
	if err != nil {
		return nil, err
	}
	svc := route53.New(route53.Options{Region: r.Region, Credentials: ec.Options().Credentials, HTTPClient: client, RetryMaxAttempts: 3})
	input := &route53.ListHostedZonesInput{MaxItems: aws.Int32(100)}
	markers := map[string]bool{}
	// ponytail: bounded whole-account scan; use per-zone jobs if the worker deadline becomes a limit.
	return infrastructurePages(ctx, r, func(page int) ([]Resource, int, error) {
		zones, e := svc.ListHostedZones(ctx, input)
		if e != nil {
			return nil, 0, e
		}
		rows := []Resource{}
		for _, zone := range zones.HostedZones {
			id := strings.TrimPrefix(aws.ToString(zone.Id), "/hostedzone/")
			if id == "" || len(id) > 64 || strings.Trim(id, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" || aws.ToString(zone.Name) == "" {
				return nil, 0, errors.New("invalid hosted zone metadata")
			}
			if kind == "dns.zone" {
				visibility := "public"
				if zone.Config != nil && zone.Config.PrivateZone {
					visibility = "private"
				}
				rows = append(rows, Resource{NativeID: id, Name: aws.ToString(zone.Name), Region: "global", Status: "present", Size: fmt.Sprintf("%s · %d record sets", visibility, aws.ToInt64(zone.ResourceRecordSetCount))})
				continue
			}
			recordsInput := &route53.ListResourceRecordSetsInput{HostedZoneId: aws.String(id), MaxItems: aws.Int32(100)}
			recordMarkers := map[string]bool{}
			records, e := infrastructurePages(ctx, r, func(n int) ([]Resource, int, error) {
				sets, e := svc.ListResourceRecordSets(ctx, recordsInput)
				if e != nil {
					return nil, 0, e
				}
				result := []Resource{}
				for _, record := range sets.ResourceRecordSets {
					if aws.ToString(record.Name) == "" || record.Type == "" {
						return nil, 0, errors.New("invalid record set metadata")
					}
					tuple, _ := json.Marshal([]string{id, aws.ToString(record.Name), string(record.Type), aws.ToString(record.SetIdentifier)})
					native := fmt.Sprintf("%s/rrset/%x", id, sha256.Sum256(tuple))
					specification := fmt.Sprintf("%s · TTL %d seconds · %d values", record.Type, aws.ToInt64(record.TTL), len(record.ResourceRecords))
					if record.AliasTarget != nil {
						specification = string(record.Type) + " · alias (TTL managed by target)"
					} else if record.TTL == nil || *record.TTL < 0 {
						return nil, 0, errors.New("missing record TTL")
					}
					displayName := aws.ToString(record.Name)
					if record.SetIdentifier != nil {
						displayName += " [" + *record.SetIdentifier + "]"
						specification += " · routing policy"
					}
					result = append(result, Resource{NativeID: native, Name: displayName, Region: "global", Status: "present", Size: specification})
				}
				if !sets.IsTruncated {
					return result, 0, nil
				}
				token, _ := json.Marshal([]string{aws.ToString(sets.NextRecordName), string(sets.NextRecordType), aws.ToString(sets.NextRecordIdentifier)})
				if aws.ToString(sets.NextRecordName) == "" || sets.NextRecordType == "" || recordMarkers[string(token)] {
					return nil, 0, errors.New("invalid record pagination")
				}
				recordMarkers[string(token)] = true
				recordsInput.StartRecordName = sets.NextRecordName
				recordsInput.StartRecordType = sets.NextRecordType
				recordsInput.StartRecordIdentifier = sets.NextRecordIdentifier
				return result, n + 1, nil
			})
			if e != nil {
				return nil, 0, e
			}
			if len(rows)+len(records) > MaxResources {
				return nil, 0, errors.New("resource limit")
			}
			rows = append(rows, records...)
		}
		if !zones.IsTruncated {
			return rows, 0, nil
		}
		marker := aws.ToString(zones.NextMarker)
		if marker == "" || markers[marker] {
			return nil, 0, errors.New("invalid zone pagination")
		}
		markers[marker] = true
		input.Marker = zones.NextMarker
		return rows, page + 1, nil
	})
}
