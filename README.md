# dnsmasq-controller Helm Chart

A Helm chart for [dnsmasq-controller](https://github.com/aenix-io/dnsmasq-controller) — a Kubernetes operator that runs DNS and DHCP services via dnsmasq, configured declaratively through Custom Resources.

## Prerequisites

- Kubernetes 1.19+
- Helm 3

## Install

```bash
helm install my-dns ./dnsmasq-controller
```

By default, only **DNS** is enabled. To enable **DHCP** as well:

```bash
helm install my-dns ./dnsmasq-controller --set dhcp.enabled=true
```

### Label your nodes

Pods only run on nodes with the `node-role.kubernetes.io/dnsmasq` label. Label the nodes where you want dnsmasq to run:

```bash
kubectl label node <node-name> node-role.kubernetes.io/dnsmasq=
```

To run on all nodes instead, disable the node selector:

```bash
helm install my-dns ./dnsmasq-controller \
  --set dns.nodeSelector=null \
  --set dhcp.nodeSelector=null
```

## Upgrade

```bash
helm upgrade my-dns ./dnsmasq-controller
```

## Uninstall

```bash
helm uninstall my-dns
```

> **Note:** CRDs are not removed on uninstall. To delete them manually:
>
> ```bash
> kubectl delete crd dnsmasqoptions.dnsmasq.kvaps.cf dhcpoptions.dnsmasq.kvaps.cf dhcphosts.dnsmasq.kvaps.cf dnshosts.dnsmasq.kvaps.cf
> ```

## Custom Resources

The controller watches 4 CRD types. After installing the chart, you configure DNS/DHCP by creating these resources.

### DnsHosts — static DNS entries

Map hostnames to IP addresses:

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

Assign fixed IPs to devices by MAC address:

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
    - macs:
        - "aa:bb:cc:dd:ee:02"
      ip: 192.168.1.51
      hostname: server2
```

### DnsmasqOptions — general dnsmasq settings (including DHCP pools)

There is no dedicated DHCP pool CRD. DHCP ranges and other dnsmasq options are configured here:

```yaml
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DnsmasqOptions
metadata:
  name: my-settings
spec:
  options:
    # DHCP pool: hand out IPs from .100 to .200, leases last 12 hours
    - key: dhcp-range
      values:
        - "192.168.1.100,192.168.1.200,12h"
    # Upstream DNS server
    - key: server
      values:
        - "8.8.8.8"
    # Local domain
    - key: domain
      values:
        - "home.local"
```

### DhcpOptions — DHCP options sent to clients

Configure what settings DHCP clients receive (gateway, DNS server, etc.):

```yaml
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DhcpOptions
metadata:
  name: my-dhcp-options
spec:
  options:
    # Default gateway
    - key: "option:router"
      values:
        - "192.168.1.1"
    # DNS server
    - key: "option:dns-server"
      values:
        - "192.168.1.1"
```

## Configuration

All values with their defaults:

| Parameter | Description | Default |
|---|---|---|
| `image.repository` | Container image | `docker.io/kvaps/dnsmasq-controller` |
| `image.tag` | Image tag (defaults to `appVersion`) | `""` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `crds.install` | Install CRDs with the chart | `true` |
| `serviceAccount.create` | Create a ServiceAccount | `true` |
| `serviceAccount.name` | ServiceAccount name (auto-generated if empty) | `""` |
| `serviceAccount.annotations` | ServiceAccount annotations | `{}` |
| `controller.name` | Controller name for CRD filtering | `""` |
| `controller.watchNamespace` | Namespace to watch (empty = all) | `""` |
| `controller.metricsAddr` | Metrics bind address | `:8080` |
| `controller.logLevel` | Log level (info, debug, error) | `""` |
| `controller.syncDelay` | Sync delay duration | `""` |
| `controller.confDir` | dnsmasq configuration directory | `""` |
| `controller.cleanup` | Clean up config files on exit | `false` |
| `dns.enabled` | Deploy the DNS DaemonSet | `true` |
| `dns.args` | Extra dnsmasq arguments for DNS | `[]` |
| `dns.resources` | CPU/memory requests and limits | `{}` |
| `dns.nodeSelector` | Node selector for DNS pods | `node-role.kubernetes.io/dnsmasq: ""` |
| `dns.tolerations` | Tolerations for DNS pods | `[]` |
| `dns.env` | Extra environment variables for DNS | `[]` |
| `dhcp.enabled` | Deploy the DHCP DaemonSet | `false` |
| `dhcp.leaderElection` | Enable leader election for DHCP | `true` |
| `dhcp.args` | Extra dnsmasq arguments for DHCP | `["--dhcp-broadcast", "--dhcp-authoritative", "--dhcp-leasefile=/dev/null"]` |
| `dhcp.resources` | CPU/memory requests and limits | `{}` |
| `dhcp.nodeSelector` | Node selector for DHCP pods | `node-role.kubernetes.io/dnsmasq: ""` |
| `dhcp.tolerations` | Tolerations for DHCP pods | `[]` |
| `dhcp.env` | Extra environment variables for DHCP | `[]` |
| `priorityClassName` | Priority class for all pods | `system-node-critical` |
| `podAnnotations` | Annotations added to all pods | `{}` |
| `podLabels` | Labels added to all pods | `{}` |

## Architecture

The chart deploys up to two **DaemonSets** (one for DNS, one for DHCP). Both use `hostNetwork: true` so they can serve DNS/DHCP directly on the node's network interfaces.

- **DNS DaemonSet** — Runs dnsmasq in DNS-only mode
- **DHCP DaemonSet** — Runs dnsmasq in DHCP-only mode with `NET_ADMIN` capability (required for DHCP). Leader election ensures only one DHCP instance responds at a time to avoid conflicts

Both DaemonSets share the same ClusterRole (read-only access to all 4 CRD types) and ServiceAccount.

## Multiple controllers

To run separate dnsmasq instances for different purposes, set `controller.name` and use the `.spec.controller` field in your CRs to route them to the right instance:

```bash
# Install a DNS-only controller
helm install dns-prod ./dnsmasq-controller --set controller.name=dns-prod

# Install a DHCP-only controller
helm install dhcp-lab ./dnsmasq-controller \
  --set dns.enabled=false \
  --set dhcp.enabled=true \
  --set controller.name=dhcp-lab
```

Then in your CRs:

```yaml
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DnsHosts
metadata:
  name: prod-hosts
spec:
  controller: dns-prod    # only picked up by the dns-prod instance
  hosts:
    - ip: 10.0.0.1
      hostnames:
        - prod-app.local
```
