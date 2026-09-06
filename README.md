# AB Provider Modules

Go libraries and command-line collectors for cloud inventory and security metadata. Built on upstream cloud SDKs, with pagination, resource normalization, and explicit handling of incomplete results.

Supports AWS, Azure, Google Cloud, Oracle Cloud Infrastructure, Linode, DigitalOcean, and Hetzner. Coverage varies by provider and API; see the package descriptions below.

## Install

```sh
go get github.com/alphabravo-oss/ab-provider-modules@v0.2.0
```

The required Go version is specified in [go.mod](go.mod).

## Choose an API

| Package | Purpose |
|---|---|
| [`inventory`](inventory) | Operational inventory for AWS, DigitalOcean, and Hetzner, selected by resource kind |
| [`providers`](providers) | Provider-specific discovery with security attributes, aliases, and resource relationships |
| [`pkg/resource`](pkg/resource) | Resource types used by provider-specific discovery |
| [`pkg/protocol`](pkg/protocol) | Single-request JSON protocol for the standalone commands |

Import the packages directly or build a standalone collector. No database or hosted service is required.

## Operational inventory

`inventory.Collect(ctx, request, client, kinds)` returns inventory records for the requested resource kinds. If any page or kind fails, it returns an error and no records, preventing partial results from being mistaken for a complete snapshot.

| Provider | Included inventory |
|---|---|
| AWS | EC2 instances, types, images, keys, placement groups, EBS volumes/snapshots, VPCs, subnets, routes, gateways, IPs, security groups, Classic and modern load balancers, RDS instances/clusters/snapshots, EKS clusters/node groups, S3 buckets/tags, Route 53 zones/record summaries |
| DigitalOcean | Droplets, types, images, backups, snapshots, volumes, VPCs, firewalls, load balancers, reserved IPv4/IPv6, keys, projects, App Platform summaries, managed databases, Kubernetes clusters/node pools, DNS and certificate summaries |
| Hetzner | Servers, types, images, backups, snapshots, volumes, networks, firewalls, load balancers, primary/floating IPs, keys, placement groups, DNS and certificate summaries |

Supply:

- A request identifying the provider and region, with explicit token or AWS credentials.
- An HTTP client with a non-nil transport and appropriate timeouts.
- A context with a deadline.
- Resource kinds such as `compute.server`, `storage.volume`, or `network.network`.

AWS requires a region and resolved credentials; this API does not use ambient credentials or assume roles. DigitalOcean and Hetzner support optional region filtering. Global resources remain included in regional scans.

Records contain native IDs, provider identities, resource kinds, regions, status, addresses, sizing summaries, and labels or named tags. Collection is bounded to 100,000 records. This API performs discovery only; it does not modify cloud resources.

## Provider-specific discovery

The `providers/<name>` packages expose `DiscoverLive` with provider-specific options. Their resource representation includes attributes, aliases, and relationships suitable for security analysis and inventory enrichment.

These collectors have different coverage from `inventory.Collect`. For example, the Hetzner provider-specific collector currently covers projects and servers, while operational inventory covers the additional resource kinds listed above. Inspect the package implementation and returned service errors for the exact scope.

Discovery can return resources alongside service errors. Preserve that distinction: partial results do not establish resource absence or successful coverage of a service. Recorded HTTP tests exercise collection behavior; they do not establish live verification of every cloud configuration.

## Standalone commands

Build a collector:

```sh
go build -o bin/ab-provider-aws ./cmd/ab-provider-aws
```

Other command suffixes are `azure`, `gcp`, `oci`, `linode`, `digitalocean`, and `hetzner`.

Commands read one JSON request from stdin and write one JSON response to stdout using `ab.provider/v1`. Requests include `apiVersion`, `requestId`, `operation`, `provider`, and provider-specific `config`. Supported operations are `discover` and `health`; both invoke discovery, so health checks require credentials and make cloud API calls. Input is limited to 64 MiB.

Commands use the provider-specific discovery API. They do not expose `inventory.Collect` over the command protocol.

## Credentials and results

Use read-only credentials with permissions appropriate to the requested scope. An [AWS permission starting point](permissions/aws-provider-read.json) is included.

Configure trusted endpoints and transports in the calling application. Handle provider errors as potentially sensitive and redact them before displaying or logging them. Authentication, authorization, storage, and decisions based on inventory remain the caller's responsibility.

## Development

```sh
go test ./...
go vet ./...
go build ./cmd/...
```

Tests use recorded responses and local HTTP servers. CI also verifies module integrity, runs the race detector, and checks known vulnerabilities.

## Updating upstream SDKs

SDKs are ordinary Go module dependencies, not vendored forks. Dependabot opens weekly dependency and GitHub Actions updates, grouping related AWS, Azure, and Google modules.

To update a specific SDK manually:

```sh
go get github.com/hetznercloud/hcloud-go/v2@latest
go mod tidy
go test ./...
go vet ./...
```

Review passing updates and publish a new semantic version tag. Applications can then update their pinned module version and run their integration checks. Go may select newer SDK versions already required by the application; some public functions expose SDK types, so review major SDK changes before upgrading.

Update the Go toolchain separately through `go.mod` and any build images. Published version tags remain immutable.

## License

[Apache License 2.0](LICENSE).
