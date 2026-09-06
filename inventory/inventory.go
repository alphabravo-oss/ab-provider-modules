// Package inventory collects operational inventory using explicit credentials.
// Callers own credential resolution, authorization, persistence and mutation.
package inventory

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

const MaxResources = 100000

type Request struct {
	Provider, Region, Credential string
	AWS                          aws.Credentials
}

// Resource contains display metadata only, never credential-bearing SDK objects.
type Resource struct {
	Catalog                                                                           bool // Provider offerings, rather than account-owned infrastructure.
	Tags                                                                              *Tags
	ProviderIdentity, Kind, NativeID, Name, Region, Status, PublicIP, PrivateIP, Size string
}
type Tags struct {
	Labels map[string]string
	Names  []string
}

var region = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// Collect returns a complete snapshot of the requested kinds, or no rows and an
// error. The caller supplies its controlled transport and deadline. AWS credentials
// must already be brokered; no ambient credential chain or role assumption is used.
func Collect(ctx context.Context, r Request, client *http.Client, kinds []string) ([]Resource, error) {
	if client == nil || client.Transport == nil || (r.Region != "" && !region.MatchString(r.Region)) || len(kinds) == 0 || len(kinds) > 64 {
		return nil, errors.New("invalid inventory configuration")
	}
	switch r.Provider {
	case "aws":
		if r.Region == "" || r.AWS.AccessKeyID == "" || r.AWS.SecretAccessKey == "" {
			return nil, errors.New("explicit AWS credentials and region required")
		}
	case "digitalocean", "hetzner":
		if r.Credential == "" || len(r.Credential) > 16384 {
			return nil, errors.New("explicit token required")
		}
	default:
		return nil, errors.New("unsupported provider")
	}
	seen := map[string]bool{}
	out := []Resource{}
	for _, kind := range kinds {
		if kind == "" || seen[kind] {
			return nil, errors.New("invalid inventory kind")
		}
		seen[kind] = true
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var rows []Resource
		var err error
		switch r.Provider {
		case "aws":
			if kind == "compute.server" {
				rows, err = awsServers(ctx, r, client)
			} else {
				rows, err = awsInfrastructure(ctx, r, client, kind)
			}
		case "digitalocean":
			if kind == "compute.server" {
				rows, err = doServers(ctx, r, client)
			} else {
				rows, err = doInfrastructure(ctx, r, client, kind)
			}
		case "hetzner":
			if kind == "compute.server" {
				rows, err = hetznerServers(ctx, r, client)
			} else {
				rows, err = hetznerInfrastructure(ctx, r, client, kind)
			}
		}
		if err != nil {
			return nil, err
		}
		if len(rows) > MaxResources-len(out) {
			return nil, errors.New("resource limit")
		}
		for j := range rows {
			rows[j].Kind = kind
			rows[j].Catalog = rows[j].Catalog || kind == "compute.type"
		}
		out = append(out, rows...)
	}
	return out, nil
}

func awsClient(r Request, client *http.Client, retries int) (*ec2.Client, error) {
	if r.AWS.AccessKeyID == "" || r.AWS.SecretAccessKey == "" {
		return nil, errors.New("explicit AWS credentials required")
	}
	return ec2.NewFromConfig(aws.Config{Region: r.Region, Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(r.AWS.AccessKeyID, r.AWS.SecretAccessKey, r.AWS.SessionToken)), HTTPClient: client, RetryMaxAttempts: retries}), nil
}
