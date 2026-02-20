# dnsmasq-controller

A Kubernetes operator that runs DNS and DHCP services via dnsmasq, configured declaratively through Custom Resources.

This is a fork of [aenix-io/dnsmasq-controller](https://github.com/aenix-io/dnsmasq-controller) with an added **DhcpPool** CRD that lets you define DHCP address ranges as a dedicated Kubernetes resource instead of using raw `dhcp-range` lines in DnsmasqOptions.

## What's in this repo

```
controller/                  Go source code (built into the container image)
charts/dnsmasq-controller/   Distributable Helm chart
dnsmasq-controller/          Development Helm chart (same templates)
```

## Prerequisites

- Kubernetes 1.19+
- Helm 3
- Docker (to build the image)
- A Kubernetes cluster (kind, minikube, etc.)

## Build the image

The image must be built locally before installing. There is no pre-built image on a registry.

```bash
docker build -t dnsmasq-controller:latest controller/
```

If using **kind**, load it into the cluster:

```bash
kind load docker-image dnsmasq-controller:latest
```

If using **minikube**:

```bash
minikube image load dnsmasq-controller:latest
```

## Install

After building the image, install with Helm:

```bash
helm install dnsmasq charts/dnsmasq-controller \
  --set image.repository=dnsmasq-controller \
  --set image.tag=latest \
  --set image.pullPolicy=Never
```

To enable DHCP as well:

```bash
helm install dnsmasq charts/dnsmasq-controller \
  --set image.repository=dnsmasq-controller \
  --set image.tag=latest \
  --set image.pullPolicy=Never \
  --set dhcp.enabled=true
```

### Label your nodes

Pods only schedule on nodes with the `node-role.kubernetes.io/dnsmasq` label:

```bash
kubectl label node <node-name> node-role.kubernetes.io/dnsmasq=
```

To run on all nodes instead, clear the node selector:

```bash
helm install dnsmasq charts/dnsmasq-controller \
  --set image.repository=dnsmasq-controller \
  --set image.tag=latest \
  --set image.pullPolicy=Never \
  --set dns.nodeSelector=null \
  --set dhcp.nodeSelector=null
```

## Upgrade

```bash
helm upgrade dnsmasq charts/dnsmasq-controller
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

The controller generates `dhcp-range=` lines in dnsmasq config from this resource.

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

## Verifying DhcpPool works

1. Build and install the chart with DHCP enabled:

```bash
docker build -t dnsmasq-controller:latest controller/
kind load docker-image dnsmasq-controller:latest
helm install dnsmasq charts/dnsmasq-controller \
  --set image.repository=dnsmasq-controller \
  --set image.tag=latest \
  --set image.pullPolicy=Never \
  --set dhcp.enabled=true
```

2. Create a DhcpPool:

```bash
kubectl apply -f - <<EOF
apiVersion: dnsmasq.kvaps.cf/v1beta1
kind: DhcpPool
metadata:
  name: test-pool
spec:
  pools:
    - rangeStart: "192.168.1.100"
      rangeEnd: "192.168.1.200"
      leaseTime: "12h"
EOF
```

3. Confirm the resource was created:

```bash
kubectl get dhcppools
```

4. Check the controller logs to see it picked up the pool:

```bash
kubectl logs deploy/dnsmasq-dnsmasq-controller-dhcp
```

You should see a line like:

```
Written /etc/dnsmasq.d/default-test-pool-pool.conf
```

5. Verify the generated config:

```bash
kubectl exec deploy/dnsmasq-dnsmasq-controller-dhcp -- cat /etc/dnsmasq.d/default-test-pool-pool.conf
```

Expected output:

```
dhcp-range=192.168.1.100,192.168.1.200,12h
```

OR Verify with following
```bash
kubectl logs deploy/dnsmasq-dnsmasq-controller-dhcp | grep -i pool
```

Expected output:

Written /etc/dnsmasq.d/default-test-dhcp-pool-pool.conf                     


6. Confirm dnsmasq reloaded by checking logs for a SIGHUP or restart message.

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
- **DHCP Deployment** runs dnsmasq in DHCP-only mode with `NET_ADMIN` capability. Leader election ensures only one DHCP instance responds at a time

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
