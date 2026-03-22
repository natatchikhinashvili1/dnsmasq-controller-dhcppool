# dnsmasq-controller

A Kubernetes operator that runs DNS and DHCP services via dnsmasq, configured declaratively through Custom Resources.

This operator includes a **DhcpPool** CRD that lets you define DHCP address ranges as a dedicated Kubernetes resource instead of using raw `dhcp-range` lines in DnsmasqOptions.

## What's in this repo

```text
.                            Go source code + Helm chart (flat layout)
├── api/                     CRD type definitions
├── controllers/             Reconciler logic
├── pkg/                     Server and utility packages
├── config/                  Kustomize manifests (CRDs, RBAC)
├── templates/               Helm chart templates
├── samples/                 Example CRs for testing
├── Makefile.maker.yaml      go-makefile-maker config (generates the Makefile)
├── Makefile                 Auto-generated — do not edit
└── .golangci.yaml           Auto-generated linter config
```

## Prerequisites

- Kubernetes v1.25+
- Helm 3
- Docker (to build the image)
- A Kubernetes cluster (kind, minikube, etc.)
- [GNU Make](https://www.gnu.org/software/make/) 4.0+ (on macOS: `brew install make`, then use `gmake`)
- Go 1.13+

Make sure Go binaries are on your PATH:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

## Quick start (build, deploy, test)

```bash
# 1. Build and lint
gmake build-all
gmake run-golangci-lint

# 2. Build the Docker image
docker build -t dnsmasq-controller:latest .

# 3. Create a kind cluster and load the image
kind create cluster
kind load docker-image dnsmasq-controller:latest

# 4. Install with Helm (DNS + DHCP)
                                                          
helm install dnsmasq . --set image.repository=dnsmasq-controller --set image.tag=latest  --set image.pullPolicy=Never   --set dhcp.enabled=true  --set dns.nodeSelector=null  --set dhcp.nodeSelector=null                            
                                   

# 5. Wait for pods
kubectl get pods -w

### Label your nodes

Pods only schedule on nodes with the `node-role.kubernetes.io/dnsmasq` label:

```bash
kubectl label node <node-name> node-role.kubernetes.io/dnsmasq=
```

To run on all nodes instead (useful for kind/minikube):

```bash
helm install dnsmasq . \
  --set image.repository=dnsmasq-controller \
  --set image.tag=latest \
  --set image.pullPolicy=Never \
  --set dns.nodeSelector=null \
  --set dhcp.nodeSelector=null
```

## Upgrade

```bash
helm upgrade dnsmasq .
```

## Uninstall

```bash
helm uninstall dnsmasq
```

CRDs are not removed on uninstall. To delete them manually:

```bash
kubectl delete crd dhcppools.dnsmasq.kvaps.cf dhcphosts.dnsmasq.kvaps.cf \
  dhcpoptions.dnsmasq.kvaps.cf dnshosts.dnsmasq.kvaps.cf dnsmasqoptions.dnsmasq.kvaps.cf
```

## Custom Resources

The controller watches 5 CRD types.

### DhcpPool — DHCP address ranges

Define DHCP pools as a dedicated resource:

```yaml
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DhcpPool
metadata:
  name: my-pool
spec:
  pools:
    - rangeStart: "192.168.1.100"
      rangeEnd: "192.168.1.200"
      leaseTime: "12h"
    - rangeStart: "10.0.0.50"
      rangeEnd: "10.0.0.150"
      netmask: "255.255.255.0"
      leaseTime: "24h"
      tag: "vlan10"
```

Each pool entry supports:

| Field | Required | Description |
|---|---|---|
| `rangeStart` | yes | Start IP of the DHCP range |
| `rangeEnd` | yes | End IP of the DHCP range |
| `leaseTime` | no | Lease duration (e.g. `12h`, `24h`, `infinite`) |
| `netmask` | no | Network mask |
| `broadcast` | no | Broadcast address |
| `tag` | no | Tag to associate with this range |
| `gateway` | no | Gateway IP address (generates `dhcp-option=option:router`) |
| `dhcpBoot` | no | PXE boot configuration (e.g. `https://boot.example.com/ipxe`) |

The controller generates `dhcp-range=` lines in dnsmasq config from this resource.

#### Automatic import from NetBox

DhcpPool supports automatic import of DHCP prefixes from NetBox. Add the optional `netboxImport` section to have the controller periodically fetch prefixes and merge them into the `pools` list (add-only: new entries are added, existing entries are never removed).

```yaml
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DhcpPool
metadata:
  name: a-qa-de-1-discovery
  namespace: metal-operator-dhcp
spec:
  controller: ""
  netboxImport:
    netboxURL: https://netbox.global.cloud.sap
    tokenSecretRef:
      name: netbox-token
      key: token
    clusterType: admin
    clusterName: a-qa-de-1
    region: qa-de-1
    syncInterval: 5m
    leaseTime: 10m
    roles:
      - roleID: 40
        name: runtime-discovery
  pools: []
```

The `pools` list is auto-populated from NetBox. You can also include manual entries alongside imported ones.

**NetBox import fields:**

| Field | Required | Description |
|---|---|---|
| `netboxURL` | yes | NetBox API base URL |
| `tokenSecretRef.name` | yes | Name of the Kubernetes Secret containing the NetBox API token |
| `tokenSecretRef.key` | yes | Key within the Secret |
| `clusterType` | yes | `admin` or `runtime` — determines the `dhcp-boot` URL pattern |
| `clusterName` | yes | Cluster name, e.g. `a-qa-de-1` |
| `region` | yes | Region to filter NetBox sites (e.g. `qa-de-1` matches sites `qa-de-1a`, `qa-de-1b`, etc.) |
| `syncInterval` | no | Re-sync interval (default: `5m`) |
| `leaseTime` | no | Default lease time for imported entries (default: `10m`) |
| `roles` | yes | List of NetBox prefix roles to import |
| `roles[].roleID` | yes | NetBox role ID (e.g. `40` for Metal Runtime Discovery, `38` for Metal Compute Discovery) |
| `roles[].name` | yes | Human-readable name for logging |

**How it works:**

1. The controller reads the NetBox API token from the referenced Secret
2. It fetches all prefixes for each configured role from the NetBox API
3. It filters prefixes by region (matching all sites that start with the region, e.g. `qa-de-1` matches `qa-de-1a`, `qa-de-1b`, `qa-de-1d`)
4. For each prefix, it calculates the DHCP pool range (starting at the 4th usable IP)
5. New entries are merged into the `pools` list (add-only)
6. The controller re-syncs on the configured interval

**Prerequisite:** Create a Kubernetes Secret with the NetBox API token:

```bash
kubectl create secret generic netbox-token \
  --namespace metal-operator-dhcp \
  --from-literal=token=<your-netbox-api-token>
```

Or use External Secrets Operator / Vault to manage the secret.

**Admin vs Runtime clusters:**

| Cluster Type | dhcp-boot URL pattern | Typical roles |
|---|---|---|
| `admin` | `https://boot-operator.admin.<region>.cloud.sap/ipxe` | Runtime Discovery (role 40) |
| `runtime` | `https://boot-operator-remote.runtime.<region>.cloud.sap/ipxe` | Runtime Discovery (role 40) + Compute Discovery (role 38) |

**Example for a runtime cluster:**

```yaml
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DhcpPool
metadata:
  name: rt-qa-de-1-discovery
  namespace: metal-operator-dhcp
spec:
  controller: ""
  netboxImport:
    netboxURL: https://netbox.global.cloud.sap
    tokenSecretRef:
      name: netbox-token
      key: token
    clusterType: runtime
    clusterName: rt-qa-de-1
    region: qa-de-1
    syncInterval: 5m
    leaseTime: 10m
    roles:
      - roleID: 40
        name: runtime-discovery
      - roleID: 38
        name: compute-discovery
  pools: []
```

#### CLI tool: netbox-importer

A standalone CLI tool is also available for one-off imports or debugging:

```bash
# Build
go build -o netbox-importer ./cmd/netbox-importer

# Dry-run (print generated YAML)
NETBOX_TOKEN=<token> ./netbox-importer \
  --cluster-type admin \
  --cluster-name a-qa-de-1 \
  --region qa-de-1 \
  --dry-run

# Apply directly to cluster
NETBOX_TOKEN=<token> ./netbox-importer \
  --cluster-type admin \
  --cluster-name a-qa-de-1 \
  --region qa-de-1
```

### DnsHosts — static DNS entries

```yaml
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DnsHosts
metadata:
  name: my-hosts
spec:
  hosts:
    - ip: 192.168.1.10
      hostnames:
        - myapp.local
        - myapp
    - ip: 192.168.1.20
      hostnames:
        - database.local
```

### DhcpHosts — static DHCP leases

```yaml
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DhcpHosts
metadata:
  name: my-reservations
spec:
  hosts:
    - macs:
        - "aa:bb:cc:dd:ee:01"
      ip: 192.168.1.50
      hostname: server1
      leaseTime: "24h"
```

### DhcpOptions — DHCP options sent to clients

```yaml
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DhcpOptions
metadata:
  name: my-dhcp-options
spec:
  options:
    - key: "option:router"
      values:
        - "192.168.1.1"
    - key: "option:dns-server"
      values:
        - "192.168.1.1"
```

### DnsmasqOptions — raw dnsmasq settings

```yaml
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DnsmasqOptions
metadata:
  name: my-settings
spec:
  options:
    - key: server
      values:
        - "8.8.8.8"
    - key: domain
      values:
        - "home.local"
```

## Where generated config lives

The controller writes dnsmasq config files inside the container at `/etc/dnsmasq.d/`:

| CRD | Config path |
|---|---|
| DhcpPool | `/etc/dnsmasq.d/<namespace>-<name>-pool.conf` |
| DnsmasqOptions | `/etc/dnsmasq.d/<namespace>-<name>.conf` |
| DnsHosts | `/etc/dnsmasq.d/hosts/<namespace>-<name>` |
| DhcpHosts | `/etc/dnsmasq.d/dhcp-hosts/<namespace>-<name>` |
| DhcpOptions | `/etc/dnsmasq.d/dhcp-opts/<namespace>-<name>` |

To inspect the generated config, exec into the pod:

```bash
kubectl exec -it deploy/dnsmasq-dnsmasq-controller-dhcp -- ls /etc/dnsmasq.d/
kubectl exec -it deploy/dnsmasq-dnsmasq-controller-dhcp -- cat /etc/dnsmasq.d/default-my-pool-pool.conf
```

## Testing all features

### Prerequisites for testing

Make sure you have a running cluster with the controller installed (see [Quick start](#quick-start-build-deploy-test) above).

Verify the pods are running:

```bash
kubectl get pods
```

You should see pods like `dnsmasq-dnsmasq-controller-dns-*` and `dnsmasq-dnsmasq-controller-dhcp-*`.

### Test 1: DhcpPool (DHCP address ranges)

```bash
kubectl apply -f samples/test-dhcppool.yaml
```

Or create one inline:

```bash
kubectl apply -f - <<'EOF'
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DhcpPool
metadata:
  name: test-pool
spec:
  pools:
    - rangeStart: "192.168.1.100"
      rangeEnd: "192.168.1.200"
      leaseTime: "12h"
    - rangeStart: "10.0.0.50"
      rangeEnd: "10.0.0.150"
      netmask: "255.255.255.0"
      gateway: "10.0.0.1"
      leaseTime: "24h"
      tag: "vlan10"
EOF
```

Verify:

```bash
# Check resource status
kubectl get dhcppools

# Check controller logs
kubectl logs deploy/dnsmasq-dnsmasq-controller-dhcp | grep -i pool

# Expected log: Written /etc/dnsmasq.d/default-test-pool-pool.conf

# Inspect the generated dnsmasq config
kubectl debug $(kubectl get pods -l role=dhcp -o jsonpath='{.items[0].metadata.name}') \
  -it --image=busybox --target=dnsmasq-controller --profile=general \
  -- cat /proc/1/root/etc/dnsmasq.d/default-test-pool-pool.conf
```

Expected config output:

```
dhcp-range=192.168.1.100,192.168.1.200,12h
dhcp-range=set:vlan10,10.0.0.50,10.0.0.150,255.255.255.0,24h
dhcp-option=tag:vlan10,option:router,10.0.0.1
```

Clean up:

```bash
kubectl delete dhcppool test-pool
```

### Test 2: DnsHosts (static DNS entries)

```bash
kubectl apply -f samples/test-dnshosts.yaml
```

Or create one inline:

```bash
kubectl apply -f - <<'EOF'
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DnsHosts
metadata:
  name: test-dns
spec:
  hosts:
    - ip: 192.168.1.10
      hostnames:
        - myapp.local
        - myapp
EOF
```

Verify:

```bash
# Check resource
kubectl get dnshosts

# Check controller logs
kubectl logs deploy/dnsmasq-dnsmasq-controller-dns | grep -i "test-dns"

# Expected log: Written /etc/dnsmasq.d/hosts/default-test-dns

# Inspect the generated hosts file
kubectl debug $(kubectl get pods -l role=dns -o jsonpath='{.items[0].metadata.name}') \
  -it --image=busybox --target=dnsmasq-controller --profile=general \
  -- cat /proc/1/root/etc/dnsmasq.d/hosts/default-test-dns
```

Expected output:

```
192.168.1.10 myapp.local myapp
```

Clean up:

```bash
kubectl delete dnshosts test-dns
```

### Test 3: DhcpHosts (static DHCP leases)

```bash
kubectl apply -f samples/test-dhcphosts.yaml
```

Or create one inline:

```bash
kubectl apply -f - <<'EOF'
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DhcpHosts
metadata:
  name: test-dhcp-hosts
spec:
  hosts:
    - macs:
        - "aa:bb:cc:dd:ee:01"
      ip: 192.168.1.50
      hostname: server1
      leaseTime: "24h"
EOF
```

Verify:

```bash
kubectl get dhcphosts
kubectl logs deploy/dnsmasq-dnsmasq-controller-dhcp | grep -i "test-dhcp-hosts"

# Expected log: Written /etc/dnsmasq.d/dhcp-hosts/default-test-dhcp-hosts
```

Clean up:

```bash
kubectl delete dhcphosts test-dhcp-hosts
```

### Test 4: DhcpOptions (DHCP options)

```bash
kubectl apply -f samples/test-dhcpoptions.yaml
```

Or create one inline:

```bash
kubectl apply -f - <<'EOF'
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DhcpOptions
metadata:
  name: test-dhcp-opts
spec:
  options:
    - key: "option:router"
      values:
        - "192.168.1.1"
    - key: "option:dns-server"
      values:
        - "8.8.8.8"
        - "8.8.4.4"
EOF
```

Verify:

```bash
kubectl get dhcpoptions
kubectl logs deploy/dnsmasq-dnsmasq-controller-dhcp | grep -i "test-dhcp-opts"

# Expected log: Written /etc/dnsmasq.d/dhcp-opts/default-test-dhcp-opts
```

Clean up:

```bash
kubectl delete dhcpoptions test-dhcp-opts
```

### Test 5: DnsmasqOptions (raw dnsmasq config)

```bash
kubectl apply -f samples/test-dnsmasqoptions.yaml
```

Or create one inline:

```bash
kubectl apply -f - <<'EOF'
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DnsmasqOptions
metadata:
  name: test-dnsmasq-opts
spec:
  options:
    - key: server
      values:
        - "8.8.8.8"
    - key: domain
      values:
        - "home.local"
EOF
```

Verify:

```bash
kubectl get dnsmasqoptions
kubectl logs deploy/dnsmasq-dnsmasq-controller-dns | grep -i "test-dnsmasq-opts"

# Expected log: Written /etc/dnsmasq.d/default-test-dnsmasq-opts.conf
```

Clean up:

```bash
kubectl delete dnsmasqoptions test-dnsmasq-opts
```

### Test 6: Update and delete

Test that config updates and cleanup work:

```bash
# Create a pool
kubectl apply -f samples/test-dhcppool.yaml

# Check logs for "Written"
kubectl logs deploy/dnsmasq-dnsmasq-controller-dhcp --tail=5

# Update it (edit rangeEnd or leaseTime)
kubectl patch dhcppool test-dhcp-pool --type=merge \
  -p '{"spec":{"pools":[{"rangeStart":"192.168.1.100","rangeEnd":"192.168.1.250","leaseTime":"1h"}]}}'

# Check logs again — should see another "Written" and "Configuration changed, restarting dnsmasq"
kubectl logs deploy/dnsmasq-dnsmasq-controller-dhcp --tail=10

# Delete it
kubectl delete dhcppool test-dhcp-pool

# Check logs — should see "Removed"
kubectl logs deploy/dnsmasq-dnsmasq-controller-dhcp --tail=5
```

### Clean up all test resources

```bash
kubectl delete -f samples/ --ignore-not-found
```

## Configuration

| Parameter | Description | Default |
|---|---|---|
| `image.repository` | Container image | `ghcr.io/natchikhin/dnsmasq-controller` (override with local build) |
| `image.tag` | Image tag (defaults to appVersion) | `""` |
| `image.pullPolicy` | Pull policy | `IfNotPresent` |
| `crds.install` | Install CRDs | `true` |
| `serviceAccount.create` | Create ServiceAccount | `true` |
| `serviceAccount.name` | ServiceAccount name | `""` |
| `serviceAccount.annotations` | ServiceAccount annotations | `{}` |
| `controller.name` | Controller name for CRD filtering | `""` |
| `controller.watchNamespace` | Namespace to watch (empty = all) | `""` |
| `controller.metricsAddr` | Metrics address | `:8080` |
| `controller.logLevel` | Log level | `""` |
| `controller.syncDelay` | Sync delay | `""` |
| `controller.confDir` | Config directory | `""` |
| `controller.cleanup` | Cleanup config on exit | `false` |
| `dns.enabled` | Deploy DNS | `true` |
| `dns.replicas` | DNS replicas | `1` |
| `dns.metricsAddr` | DNS metrics address | `""` |
| `dns.args` | Extra dnsmasq args for DNS | `[]` |
| `dns.resources` | DNS resources | `{}` |
| `dns.nodeSelector` | DNS node selector | `node-role.kubernetes.io/dnsmasq: ""` |
| `dns.tolerations` | DNS tolerations | `[]` |
| `dns.env` | DNS extra env vars | `[]` |
| `dhcp.enabled` | Deploy DHCP | `false` |
| `dhcp.replicas` | DHCP replicas | `1` |
| `dhcp.metricsAddr` | DHCP metrics address | `:8081` |
| `dhcp.leaderElection` | Leader election for DHCP | `true` |
| `dhcp.args` | Extra dnsmasq args for DHCP | `["--dhcp-broadcast", "--dhcp-authoritative", "--dhcp-leasefile=/dev/null"]` |
| `dhcp.resources` | DHCP resources | `{}` |
| `dhcp.nodeSelector` | DHCP node selector | `node-role.kubernetes.io/dnsmasq: ""` |
| `dhcp.tolerations` | DHCP tolerations | `[]` |
| `dhcp.env` | DHCP extra env vars | `[]` |
| `priorityClassName` | Priority class | `system-node-critical` |
| `podAnnotations` | Pod annotations | `{}` |
| `podLabels` | Pod labels | `{}` |

## Architecture

The chart deploys up to two Deployments (DNS and DHCP). Both use `hostNetwork: true` so they serve DNS/DHCP directly on the node's network interfaces.

- **DNS Deployment** runs dnsmasq in DNS-only mode
- **DHCP Deployment** runs dnsmasq in DHCP-only mode with `NET_ADMIN` and `NET_BIND_SERVICE` capabilities. Leader election ensures only one DHCP instance responds at a time
Both share the same ClusterRole (read-only access to all 5 CRD types) and ServiceAccount.

## Multiple controllers

To run separate dnsmasq instances, set `controller.name` and use `.spec.controller` in your CRs:

```bash
helm install dns-prod charts/dnsmasq-controller \
  --set image.repository=dnsmasq-controller \
  --set image.tag=latest \
  --set image.pullPolicy=Never \
  --set controller.name=dns-prod

helm install dhcp-lab charts/dnsmasq-controller \
  --set image.repository=dnsmasq-controller \
  --set image.tag=latest \
  --set image.pullPolicy=Never \
  --set dns.enabled=false \
  --set dhcp.enabled=true \
  --set controller.name=dhcp-lab
```

```yaml
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DnsHosts
metadata:
  name: prod-hosts
spec:
  controller: dns-prod
  hosts:
    - ip: 10.0.0.1
      hostnames:
        - prod-app.local
```

## Development

### Makefile generation with go-makefile-maker

This project uses [go-makefile-maker](https://github.com/sapcc/go-makefile-maker) to generate the `Makefile`. **Do not edit the `Makefile` directly** — it will be overwritten.

Edit `Makefile.maker.yaml` and regenerate:

```bash
# Install (one-time)
go install github.com/sapcc/go-makefile-maker@latest

# Regenerate after changing Makefile.maker.yaml
go-makefile-maker
```

### Common development commands

On macOS, use `gmake` instead of `make`.

```bash
gmake help              # Show all targets
gmake build-all         # Build the binary
gmake generate          # Run controller-gen (CRDs, RBAC, deepcopy)
gmake run-golangci-lint # Lint the code
gmake static-check      # All static checks (lint + shellcheck)
gmake goimports         # Fix import ordering
gmake tidy-deps         # go mod tidy + verify
gmake check             # Full test suite + checks
```

See `controller-README.md` for more details.
