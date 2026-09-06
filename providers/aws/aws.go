package aws

import (
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/configservice"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

type Fixture struct {
	Account                string                                             `json:"account"`
	ListBuckets            s3.ListBucketsOutput                               `json:"listBuckets"`
	PublicAccessBlock      map[string]s3.GetPublicAccessBlockOutput           `json:"publicAccessBlock"`
	FailedPAB              []string                                           `json:"failedPublicAccessBlock"`
	AccountSummary         iam.GetAccountSummaryOutput                        `json:"accountSummary"`
	PasswordPolicy         iam.GetAccountPasswordPolicyOutput                 `json:"passwordPolicy"`
	ECRImageDigests        []string                                           `json:"ecrImageDigests"`
	DescribeTrails         cloudtrail.DescribeTrailsOutput                    `json:"describeTrails"`
	EventSelectors         map[string]cloudtrail.GetEventSelectorsOutput      `json:"eventSelectors"`
	DescribeRecorders      configservice.DescribeConfigurationRecordersOutput `json:"describeRecorders"`
	DescribeNetworkAcls    ec2.DescribeNetworkAclsOutput                      `json:"describeNetworkAcls"`
	DescribeSecurityGroups ec2.DescribeSecurityGroupsOutput                   `json:"describeSecurityGroups"`
	DescribeInstances      ec2.DescribeInstancesOutput                        `json:"describeInstances"`
	DescribeVolumes        ec2.DescribeVolumesOutput                          `json:"describeVolumes"`
	DescribeVpcs           ec2.DescribeVpcsOutput                             `json:"describeVpcs"`
	DescribeFlowLogs       ec2.DescribeFlowLogsOutput                         `json:"describeFlowLogs"`
	DescribeRouteTables    ec2.DescribeRouteTablesOutput                      `json:"describeRouteTables"`
	DescribeFileSystems    efs.DescribeFileSystemsOutput                      `json:"describeFileSystems"`
	ListKeys               kms.ListKeysOutput                                 `json:"listKeys"`
	KeyRotation            map[string]kms.GetKeyRotationStatusOutput          `json:"keyRotation"`
	DescribeDBInstances    rds.DescribeDBInstancesOutput                      `json:"describeDBInstances"`
	MetricFilters          cloudwatchlogs.DescribeMetricFiltersOutput         `json:"metricFilters"`
	ListUsers              iam.ListUsersOutput                                `json:"listUsers"`
	UserMFA                map[string]iam.ListMFADevicesOutput                `json:"userMfa"`
	UserKeys               map[string]iam.ListAccessKeysOutput                `json:"userKeys"`
	UserKeyLastUsed        map[string]iam.GetAccessKeyLastUsedOutput          `json:"userKeyLastUsed"`
	GetBucketLogging       map[string]s3.GetBucketLoggingOutput               `json:"getBucketLogging"`
	GetBucketVersioning    map[string]s3.GetBucketVersioningOutput            `json:"getBucketVersioning"`
	GetBucketPolicy        map[string]s3.GetBucketPolicyOutput                `json:"getBucketPolicy"`
	// Account-level Get* bodies (official JSON field names from those APIs).
	AlternateContact struct {
		Name, EmailAddress string
	} `json:"alternateContact"`
	Contact struct {
		FullName string
	} `json:"contact"`
	ListSAMLProviders iam.ListSAMLProvidersOutput `json:"listSamlProviders"`
	MacieStatus       string                      `json:"macieStatus"`
	SecurityHubArn    string                      `json:"securityHubArn"`
	AccessAnalyzer    struct {
		Analyzers []struct{ Status string }
	} `json:"listAnalyzers"`
	HardwareMFA           bool   `json:"hardwareMfa"`
	SupportRole           bool   `json:"supportRole"`
	AWSAdminPolicy        bool   `json:"awsAdminPolicy"`
	CustomerAdminPolicy   bool   `json:"customerAdminPolicy"`
	ExpiredCerts          int    `json:"expiredCerts"`
	UsersAttachedPolicies int    `json:"usersAttachedPolicies"`
	StaleAccessKeys       int    `json:"staleAccessKeys"`
	AccountIMDSv2         string `json:"accountImdsv2"`
	UnrestrictedHost      bool   `json:"unrestrictedHostIngress"`
	SecurityQuestions     bool   `json:"securityQuestions"`
	RootUsed              bool   `json:"rootUsed"`
}

func DiscoverJSON(raw []byte) ([]resource.Resource, error) {
	rs, _, err := Discover(raw)
	return rs, err
}

func Discover(raw []byte) ([]resource.Resource, []string, error) {
	var f Fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, nil, err
	}
	return resourcesFrom(f)
}

func resourcesFrom(f Fixture) ([]resource.Resource, []string, error) {
	acct := f.Account
	if acct == "" && f.ListBuckets.Owner != nil {
		acct = aws.ToString(f.ListBuckets.Owner.ID)
	}
	var errs []string
	failedPAB := map[string]bool{}
	for _, n := range f.FailedPAB {
		failedPAB[n] = true
		errs = append(errs, "s3 GetPublicAccessBlock "+n)
	}
	acctRes := accountResource(acct, f)
	out := []resource.Resource{acctRes}
	for _, b := range f.ListBuckets.Buckets {
		name := aws.ToString(b.Name)
		r := FromBucket(b, acct)
		if !failedPAB[name] {
			if pab, ok := f.PublicAccessBlock[name]; ok && pab.PublicAccessBlockConfiguration != nil {
				cfg := pab.PublicAccessBlockConfiguration
				r.Attrs = map[string]any{
					"public_access_block": map[string]any{
						"ignore_public_acls":      aws.ToBool(cfg.IgnorePublicAcls),
						"restrict_public_buckets": aws.ToBool(cfg.RestrictPublicBuckets),
					},
				}
			}
		}
		if r.Attrs == nil {
			r.Attrs = map[string]any{}
		}
		if lg, ok := f.GetBucketLogging[name]; ok {
			r.Attrs["access_logging"] = lg.LoggingEnabled != nil
		}
		if ver, ok := f.GetBucketVersioning[name]; ok && ver.MFADelete != "" {
			r.Attrs["mfa_delete"] = string(ver.MFADelete) == "Enabled"
		}
		if pol, ok := f.GetBucketPolicy[name]; ok && pol.Policy != nil {
			r.Attrs["secure_transport"] = containsAWS(*pol.Policy, "aws:SecureTransport")
		}
		out = append(out, r)
	}
	for _, d := range f.ECRImageDigests {
		out = append(out, resource.Resource{
			Type: "container_image", UID: d, Account: acct,
			Aliases: []resource.Alias{{Kind: "image_digest", Value: d}},
		})
	}
	out = append(out, mapTrails(acct, f)...)
	out = append(out, mapEC2(acct, f)...)
	out = append(out, mapData(acct, f)...)
	return out, errs, nil
}

func containsAWS(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()))
}

func accountResource(acct string, f Fixture) resource.Resource {
	mfa, rootKeys := false, false
	if f.AccountSummary.SummaryMap != nil {
		if v, ok := f.AccountSummary.SummaryMap["AccountMFAEnabled"]; ok {
			mfa = v > 0
		}
		if v, ok := f.AccountSummary.SummaryMap["AccountAccessKeysPresent"]; ok {
			rootKeys = v > 0
		}
	}
	min14, reuse24 := false, false
	if p := f.PasswordPolicy.PasswordPolicy; p != nil {
		if p.MinimumPasswordLength != nil && aws.ToInt32(p.MinimumPasswordLength) >= 14 {
			min14 = true
		}
		if p.PasswordReusePrevention != nil && aws.ToInt32(p.PasswordReusePrevention) >= 24 {
			reuse24 = true
		}
	}
	aa := false
	for _, a := range f.AccessAnalyzer.Analyzers {
		if a.Status == "ACTIVE" {
			aa = true
		}
	}
	return resource.Resource{
		Type: "aws.account", UID: acct, Account: acct,
		Aliases: []resource.Alias{{Kind: "aws_account_id", Value: acct}},
		Attrs: map[string]any{
			"account": map[string]any{
				"mfa_enabled":             mfa,
				"root_access_key_present": rootKeys,
				"hardware_mfa_enabled":    f.HardwareMFA,
				"root_used":               f.RootUsed,
			},
			"password_policy": map[string]any{
				"minimum_length_14": min14,
				"reuse_24":          reuse24,
			},
			"access_analyzer_active":         aa,
			"macie_status":                   f.MacieStatus,
			"securityhub_arn":                f.SecurityHubArn,
			"contact":                        map[string]any{"full_name": f.Contact.FullName},
			"security_contact":               map[string]any{"email": f.AlternateContact.EmailAddress},
			"security_questions":             f.SecurityQuestions,
			"saml_provider_count":            len(f.ListSAMLProviders.SAMLProviderList),
			"support_role":                   f.SupportRole,
			"aws_admin_policy_attached":      f.AWSAdminPolicy,
			"customer_admin_policy_attached": f.CustomerAdminPolicy,
			"expired_server_certs":           f.ExpiredCerts,
			"users_with_attached_policies":   f.UsersAttachedPolicies,
			"stale_access_keys":              f.StaleAccessKeys,
			"account_imdsv2":                 f.AccountIMDSv2,
			"unrestricted_host_ingress":      f.UnrestrictedHost,
		},
	}
}

func FromBucket(b types.Bucket, account string) resource.Resource {
	name := aws.ToString(b.Name)
	uid := "arn:aws:s3:::" + name
	return resource.Resource{
		Type: "aws.s3_bucket", UID: uid, Account: account,
		Aliases: []resource.Alias{
			{Kind: "cloud_arn", Value: uid},
			{Kind: "aws_account_id", Value: account},
		},
		Edges: []resource.Edge{{From: account, To: uid, Kind: "contains"}},
	}
}
