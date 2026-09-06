package aws

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

type LiveOpts struct {
	HTTPClient        *http.Client
	Credentials       aws.CredentialsProvider
	Endpoint          string
	Region            string
	Profile           string
	RoleARN           string
	ExternalID        string
	RoleSessionName   string
	AccessKey, Secret string
	Session           string
}

func DiscoverLive(ctx context.Context, opt LiveOpts) ([]resource.Resource, []string, error) {
	if (opt.AccessKey == "") != (opt.Secret == "") {
		return nil, nil, fmt.Errorf("AWS access key ID and secret access key must be supplied together")
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{}
	if opt.Region != "" {
		loadOptions = append(loadOptions, awsconfig.WithRegion(opt.Region))
	}
	if opt.Profile != "" {
		loadOptions = append(loadOptions, awsconfig.WithSharedConfigProfile(opt.Profile))
	}
	if opt.HTTPClient != nil {
		loadOptions = append(loadOptions, awsconfig.WithHTTPClient(opt.HTTPClient))
	}
	if opt.Credentials != nil {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(opt.Credentials))
	} else if opt.AccessKey != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(opt.AccessKey, opt.Secret, opt.Session),
		))
	} else if opt.Endpoint != "" {
		// Explicit endpoint overrides are a test/development facility. Supplying
		// deterministic credentials keeps local SDK protocol fixtures hermetic;
		// production endpoints always use the official default credential chain.
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("AB_PROVIDER_TEST", "AB_PROVIDER_TEST", ""),
		))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if opt.RoleARN != "" {
		stsClient := sts.NewFromConfig(cfg, func(o *sts.Options) {
			if opt.Endpoint != "" {
				o.BaseEndpoint = aws.String(opt.Endpoint)
			}
		})
		provider := stscreds.NewAssumeRoleProvider(stsClient, opt.RoleARN, func(o *stscreds.AssumeRoleOptions) {
			if opt.ExternalID != "" {
				o.ExternalID = aws.String(opt.ExternalID)
			}
			if opt.RoleSessionName != "" {
				o.RoleSessionName = opt.RoleSessionName
			} else {
				o.RoleSessionName = "ab-provider"
			}
		})
		cfg.Credentials = aws.NewCredentialsCache(provider)
	}
	s3c := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if opt.Endpoint != "" {
			o.BaseEndpoint = aws.String(opt.Endpoint)
			o.UsePathStyle = true
		}
	})
	iamc := iam.NewFromConfig(cfg, func(o *iam.Options) {
		if opt.Endpoint != "" {
			o.BaseEndpoint = aws.String(opt.Endpoint)
		}
	})
	f := Fixture{Account: "", PublicAccessBlock: map[string]s3.GetPublicAccessBlockOutput{}}
	pager := s3.NewListBucketsPaginator(s3c, &s3.ListBucketsInput{})
	var lb s3.ListBucketsOutput
	for pager.HasMorePages() {
		page, pageErr := pager.NextPage(ctx)
		if pageErr != nil {
			return nil, []string{"s3 ListBuckets " + pageErr.Error()}, pageErr
		}
		lb.Buckets = append(lb.Buckets, page.Buckets...)
		if lb.Owner == nil {
			lb.Owner = page.Owner
		}
	}
	f.ListBuckets = lb
	var errs []string
	for _, b := range lb.Buckets {
		name := aws.ToString(b.Name)
		pab, err := s3c.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: b.Name})
		if err != nil {
			errs = append(errs, "s3 GetPublicAccessBlock "+name)
			f.FailedPAB = append(f.FailedPAB, name)
			continue
		}
		f.PublicAccessBlock[name] = *pab
	}
	sum, err := iamc.GetAccountSummary(ctx, &iam.GetAccountSummaryInput{})
	if err != nil {
		errs = append(errs, "iam GetAccountSummary "+err.Error())
	} else {
		f.AccountSummary = *sum
	}
	pol, err := iamc.GetAccountPasswordPolicy(ctx, &iam.GetAccountPasswordPolicyInput{})
	if err != nil {
		errs = append(errs, "iam GetAccountPasswordPolicy "+err.Error())
	} else {
		f.PasswordPolicy = *pol
	}
	rs, more, err := resourcesFrom(f)
	errs = append(errs, more...)
	return rs, errs, err
}
