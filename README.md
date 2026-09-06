# AB provider modules

Shared Go cloud discovery for AlphaBravo products. Provider packages collect cloud data; consuming products own authorization, persistence, policy, operations and UI.

Derived from `AlphaBravoCompany/sierra-scan-provider-modules` commit `38a085abaacdab74c20257db1962eac0ecb8bd69`. This is a clean source export; the original private repository and Sierra consumer are unchanged.

## Included providers

AWS, Azure, GCP, OCI, Linode, DigitalOcean and Hetzner collectors are included. Coverage differs by provider; this is not a claim of complete cloud API support or live verification. Hetzner's normalized discovery currently covers projects and servers.

Import the Go packages directly inside a product's worker, or build the `cmd/ab-provider-*` commands. Standalone commands accept one bounded JSON request over stdin/stdout using `ab.provider/v1` (`discover` or `health`). The protocol and command names have no Sierra compatibility aliases.

`providers/hetzner.ListServers` supplies complete SDK server records with an explicit resource bound. It discards results on any page failure or invalid pagination. The operational inventory collector reuses this function; Providah consumes `inventory.Collect`.

Normalized `DiscoverLive` results may contain partial service errors. Callers must preserve these errors and must not interpret incomplete discovery as resource deletion or a clean security scan. The generic resource format includes security-oriented attributes inherited from the original collectors; consumers need not adopt it as their database model.

## Build and check

Go version is pinned by `go.mod`.

```sh
go test ./...
go vet ./...
go build ./cmd/...
```

Checks use recorded/local HTTP responses, not live cloud mutations. No hosted provider service or package installation framework is required. Consumers pin an exact module version and compile only the packages they use into their own workers.

Use read-only provider credentials. The AWS permission starting point is in `permissions/aws-provider-read.json`; verify permissions against the specific collection scope. Product-controlled endpoints, transport and credential policies remain the consumer's responsibility.

## Operational inventory

`inventory.Collect` supplies the full existing Providah discovery coverage for AWS, DigitalOcean and Hetzner through one Go API. It returns display records for explicitly requested resource kinds, or an error with no records if a page/kind fails. It has no dependency on Providah, its database or its worker protocol.

| Provider | Included inventory |
|---|---|
| AWS | EC2 instances, types, images, keys, placement groups, EBS volumes/snapshots, VPC/subnet/routing/gateway/IP/security-group metadata, both load-balancer generations, RDS instances/clusters/snapshots, EKS clusters/node groups, S3 buckets/tags, Route 53 zones/record summaries |
| DigitalOcean | Droplets/types/images/backups/snapshots, volumes, VPCs/firewalls/load balancers, reserved IPv4/IPv6, keys, projects, App Platform summaries, managed databases, Kubernetes clusters/node pools, DNS and certificate summaries |
| Hetzner | Servers/types/images/backups/snapshots, volumes, networks/firewalls/load balancers, primary/floating IPs, keys, placement groups, DNS and certificate summaries |

Pass explicit token or already-brokered AWS credentials, a controlled HTTP client with a non-nil transport, a context deadline and the requested kinds. AWS requires a region; other collectors can filter by region. Global records remain in regional scans. A 100,000-record safety bound applies; this is a worker memory bound, not a commercial system limit.

Returned rows preserve native IDs, provider identities, region, status, addresses, display sizing and label/named-tag distinctions. Consumers still validate their own wire records and control inventory retirement. Provider errors are for internal handling; redact before exposing them to users. Discovery includes no mutations, metrics queries, authorization, approvals or operation reconciliation.

The `providers/*` packages retain the existing security-enriched discovery surface; `inventory` is the operational projection used by Providah. No runtime plugin or service is required. All original Providah discovery transport checks exercise this API through the consuming worker adapter; the shared module also checks all three server collectors and pagination failure behavior.

## SDK updates and releases

Upstream cloud SDKs stay ordinary Go module dependencies; this repository adds collection, pagination and normalization rather than vendoring or forking SDKs. Some collectors also use provider HTTP APIs where appropriate. Public functions may expose upstream SDK types, so a major SDK change requires consumer review.

Dependabot opens weekly Go dependency and GitHub Actions update PRs. AWS, Azure and Google module families are grouped to reduce mismatched SDK updates. CI verifies modules, runs recorded adapter checks and Go vet, and checks known vulnerabilities. Review and merge passing updates, then publish a new immutable semantic version tag. No automatic merge or floating dependency in consuming releases.

For a targeted manual SDK update:

```sh
go get github.com/hetznercloud/hcloud-go/v2@latest
go mod tidy
go test ./...
go vet ./...
```

After a tested module release, consumers update explicitly:

```sh
go get github.com/alphabravo-oss/ab-provider-modules@v0.2.0
go mod tidy
```

Run the consumer's affected worker checks too: Go may select newer SDK versions already required by that product. Existing releases remain pinned until updated. Go toolchain updates are a separate `go.mod`/build-image change; dependency PRs are not a promise to update the compiler automatically.

Licensed under Apache-2.0; see [LICENSE](LICENSE).
