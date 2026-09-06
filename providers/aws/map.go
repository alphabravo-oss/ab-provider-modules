package aws

import (
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/alphabravo-oss/ab-provider-modules/pkg/resource"
)

func peeringLeastPrivilege(f Fixture, vpcID string) (ok, present bool) {
	for _, rt := range f.DescribeRouteTables.RouteTables {
		if aws.ToString(rt.VpcId) != vpcID {
			continue
		}
		present = true
		for _, r := range rt.Routes {
			if aws.ToString(r.DestinationCidrBlock) == "0.0.0.0/0" && aws.ToString(r.VpcPeeringConnectionId) != "" {
				return false, true
			}
		}
	}
	return true, present
}

func daysSince(t time.Time) int {
	d := int(time.Since(t).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

func mapTrails(acct string, f Fixture) []resource.Resource {
	var out []resource.Resource
	for _, t := range f.DescribeTrails.TrailList {
		name := aws.ToString(t.Name)
		uid := aws.ToString(t.TrailARN)
		if uid == "" {
			uid = "arn:aws:cloudtrail:::" + name
		}
		read, write := false, false
		if sel, ok := f.EventSelectors[name]; ok {
			for _, es := range sel.EventSelectors {
				for _, dr := range es.DataResources {
					if aws.ToString(dr.Type) == "AWS::S3::Object" {
						if es.ReadWriteType == "ReadOnly" || es.ReadWriteType == "All" {
							read = true
						}
						if es.ReadWriteType == "WriteOnly" || es.ReadWriteType == "All" {
							write = true
						}
					}
				}
			}
		}
		out = append(out, res(acct, "aws.cloudtrail", uid, map[string]any{
			"is_multi_region":             aws.ToBool(t.IsMultiRegionTrail),
			"log_file_validation_enabled": aws.ToBool(t.LogFileValidationEnabled),
			"kms_key_id":                  aws.ToString(t.KmsKeyId),
			"data_events_read":            read,
			"data_events_write":           write,
		}))
	}
	if recs := f.DescribeRecorders.ConfigurationRecorders; len(recs) > 0 {
		all, glob := false, false
		if recs[0].RecordingGroup != nil {
			all = recs[0].RecordingGroup.AllSupported
			glob = recs[0].RecordingGroup.IncludeGlobalResourceTypes
		}
		out = append(out, res(acct, "aws.config", "config:"+acct, map[string]any{
			"all_supported": all, "include_global": glob,
		}))
	}
	var patterns []any
	for _, mf := range f.MetricFilters.MetricFilters {
		patterns = append(patterns, aws.ToString(mf.FilterPattern))
	}
	if len(patterns) > 0 {
		out = append(out, res(acct, "aws.cloudwatch", "cloudwatch:"+acct, map[string]any{"filter_patterns": patterns}))
	}
	return out
}

func mapEC2(acct string, f Fixture) []resource.Resource {
	var out []resource.Resource
	for _, n := range f.DescribeNetworkAcls.NetworkAcls {
		id := aws.ToString(n.NetworkAclId)
		var ents []any
		for _, e := range n.Entries {
			ents = append(ents, map[string]any{
				"egress":      aws.ToBool(e.Egress),
				"rule_action": string(e.RuleAction),
				"cidr_block":  aws.ToString(e.CidrBlock),
				"protocol":    aws.ToString(e.Protocol),
				"from_port":   port(e.PortRange, true),
				"to_port":     port(e.PortRange, false),
			})
		}
		out = append(out, res(acct, "aws.nacl", id, map[string]any{"entries": ents}))
	}
	for _, g := range f.DescribeSecurityGroups.SecurityGroups {
		id := aws.ToString(g.GroupId)
		var perms []any
		for _, p := range g.IpPermissions {
			var ranges []any
			for _, ip := range p.IpRanges {
				ranges = append(ranges, map[string]any{"cidr_ip": aws.ToString(ip.CidrIp)})
			}
			perms = append(perms, map[string]any{
				"ip_protocol": aws.ToString(p.IpProtocol),
				"from_port":   aws.ToInt32(p.FromPort),
				"to_port":     aws.ToInt32(p.ToPort),
				"ip_ranges":   ranges,
			})
		}
		out = append(out, res(acct, "aws.sg", id, map[string]any{
			"group_name":     aws.ToString(g.GroupName),
			"ip_permissions": perms,
		}))
	}
	for _, resv := range f.DescribeInstances.Reservations {
		for _, inst := range resv.Instances {
			id := aws.ToString(inst.InstanceId)
			tok := ""
			if inst.MetadataOptions != nil {
				tok = string(inst.MetadataOptions.HttpTokens)
			}
			prof := ""
			if inst.IamInstanceProfile != nil {
				prof = aws.ToString(inst.IamInstanceProfile.Arn)
			}
			out = append(out, res(acct, "aws.instance", id, map[string]any{"http_tokens": tok, "iam_profile": prof}))
		}
	}
	for _, v := range f.DescribeVolumes.Volumes {
		out = append(out, res(acct, "aws.volume", aws.ToString(v.VolumeId), map[string]any{"encrypted": aws.ToBool(v.Encrypted)}))
	}
	flow := map[string]bool{}
	for _, fl := range f.DescribeFlowLogs.FlowLogs {
		flow[aws.ToString(fl.ResourceId)] = true
	}
	for _, v := range f.DescribeVpcs.Vpcs {
		id := aws.ToString(v.VpcId)
		attrs := map[string]any{"flow_logs_enabled": flow[id]}
		if ok, present := peeringLeastPrivilege(f, id); present {
			attrs["peering_least_privilege"] = ok
		}
		out = append(out, res(acct, "aws.vpc", id, attrs))
	}
	return out
}

func mapData(acct string, f Fixture) []resource.Resource {
	var out []resource.Resource
	for _, fs := range f.DescribeFileSystems.FileSystems {
		out = append(out, res(acct, "aws.efs", aws.ToString(fs.FileSystemId), map[string]any{"encrypted": aws.ToBool(fs.Encrypted)}))
	}
	for _, k := range f.ListKeys.Keys {
		id := aws.ToString(k.KeyId)
		rot := false
		if kr, ok := f.KeyRotation[id]; ok {
			rot = kr.KeyRotationEnabled
		}
		out = append(out, res(acct, "aws.kms", id, map[string]any{"key_rotation_enabled": rot}))
	}
	for _, d := range f.DescribeDBInstances.DBInstances {
		out = append(out, res(acct, "aws.rds", aws.ToString(d.DBInstanceIdentifier), map[string]any{
			"storage_encrypted":          aws.ToBool(d.StorageEncrypted),
			"publicly_accessible":        aws.ToBool(d.PubliclyAccessible),
			"auto_minor_version_upgrade": aws.ToBool(d.AutoMinorVersionUpgrade),
		}))
	}
	for _, u := range f.ListUsers.Users {
		name := aws.ToString(u.UserName)
		mfa := false
		if md, ok := f.UserMFA[name]; ok {
			mfa = len(md.MFADevices) > 0
		}
		nkeys := 0
		if kd, ok := f.UserKeys[name]; ok {
			nkeys = len(kd.AccessKeyMetadata)
		}
		attrs := map[string]any{
			"mfa_enabled":        mfa,
			"active_access_keys": nkeys,
		}
		if u.PasswordLastUsed != nil {
			attrs["password_enabled"] = true
			attrs["password_last_used_days"] = daysSince(*u.PasswordLastUsed)
		}
		if lu, ok := f.UserKeyLastUsed[name]; ok && lu.AccessKeyLastUsed != nil && lu.AccessKeyLastUsed.LastUsedDate != nil {
			attrs["access_key_last_used_days"] = daysSince(*lu.AccessKeyLastUsed.LastUsedDate)
		}
		if u.CreateDate != nil {
			if kd, ok := f.UserKeys[name]; ok {
				for _, k := range kd.AccessKeyMetadata {
					if k.CreateDate != nil {
						d := k.CreateDate.Sub(*u.CreateDate)
						if d < 0 {
							d = -d
						}
						attrs["access_key_during_create"] = d < 24*time.Hour
						break
					}
				}
			}
		}
		out = append(out, res(acct, "aws.iam_user", "user:"+name, attrs))
	}
	return out
}

func port(pr *types.PortRange, from bool) int32 {
	if pr == nil {
		return 0
	}
	if from {
		return aws.ToInt32(pr.From)
	}
	return aws.ToInt32(pr.To)
}

func res(acct, typ, uid string, attrs map[string]any) resource.Resource {
	return resource.Resource{
		Type: typ, UID: uid, Account: acct, Attrs: attrs,
		Aliases: []resource.Alias{{Kind: "cloud_arn", Value: uid}, {Kind: "aws_account_id", Value: acct}},
		Edges:   []resource.Edge{{From: acct, To: uid, Kind: "contains"}},
	}
}
