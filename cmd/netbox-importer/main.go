package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dnsmasqv1beta1 "github.com/kvaps/dnsmasq-controller/api/v1beta1"
	"github.com/kvaps/dnsmasq-controller/pkg/netbox"
)

func main() {
	var (
		netboxURL   string
		netboxToken string
		clusterType string
		clusterName string
		region      string
		namespace   string
		controller  string
		leaseTime   string
		dryRun      bool
		outputJSON  bool
	)

	flag.StringVar(&netboxURL, "netbox-url", "https://netbox.global.cloud.sap", "NetBox API base URL")
	flag.StringVar(&netboxToken, "netbox-token", "", "NetBox API token (or set NETBOX_TOKEN env var)")
	flag.StringVar(&clusterType, "cluster-type", "", "Cluster type: 'admin' or 'runtime' (required)")
	flag.StringVar(&clusterName, "cluster-name", "", "Cluster name, e.g. 'a-qa-de-1' or 'rt-qa-de-1' (required)")
	flag.StringVar(&region, "region", "", "Region, e.g. 'qa-de-1' — used to filter NetBox sites like QA-DE-1a, QA-DE-1b (required)")
	flag.StringVar(&namespace, "namespace", "metal-operator-dhcp", "Target namespace for DhcpPool CRs")
	flag.StringVar(&controller, "controller", "", "Controller name for spec.controller")
	flag.StringVar(&leaseTime, "lease-time", "10m", "Default DHCP lease time")
	flag.BoolVar(&dryRun, "dry-run", false, "Only print generated YAML, do not apply to cluster")
	flag.BoolVar(&outputJSON, "json", false, "Output as JSON instead of YAML (only used with --dry-run)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: netbox-importer [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Imports DHCP pool prefixes from NetBox and applies DhcpPool CRs to the cluster.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  # Admin cluster — auto-apply\n")
		fmt.Fprintf(os.Stderr, "  netbox-importer --cluster-type admin --cluster-name a-qa-de-1 --region qa-de-1\n\n")
		fmt.Fprintf(os.Stderr, "  # Runtime cluster — auto-apply\n")
		fmt.Fprintf(os.Stderr, "  netbox-importer --cluster-type runtime --cluster-name rt-qa-de-1 --region qa-de-1\n\n")
		fmt.Fprintf(os.Stderr, "  # Preview without applying\n")
		fmt.Fprintf(os.Stderr, "  netbox-importer --cluster-type admin --cluster-name a-qa-de-1 --region qa-de-1 --dry-run\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Token from flag or env (supports both NETBOX_TOKEN and NETBOX_API_KEY).
	if netboxToken == "" {
		netboxToken = os.Getenv("NETBOX_TOKEN")
	}
	if netboxToken == "" {
		netboxToken = os.Getenv("NETBOX_API_KEY")
	}
	if netboxToken == "" {
		fatalf("--netbox-token or NETBOX_TOKEN/NETBOX_API_KEY env var is required")
	}

	if clusterType == "" {
		fatalf("--cluster-type is required (admin or runtime)")
	}
	if clusterType != "admin" && clusterType != "runtime" {
		fatalf("--cluster-type must be 'admin' or 'runtime', got %q", clusterType)
	}
	if clusterName == "" {
		fatalf("--cluster-name is required")
	}
	if region == "" {
		fatalf("--region is required")
	}

	cfg := &netbox.ImportConfig{
		ClusterType: netbox.ClusterType(clusterType),
		ClusterName: clusterName,
		Region:      region,
		Namespace:   namespace,
		Controller:  controller,
		LeaseTime:   leaseTime,
	}

	nbClient := netbox.NewClient(netboxURL, netboxToken)

	// Determine which roles to import based on cluster type.
	type roleImport struct {
		RoleID int
		Name   string
	}

	var roles []roleImport
	switch cfg.ClusterType {
	case netbox.ClusterTypeAdmin:
		roles = []roleImport{
			{RoleID: netbox.RoleMetalRuntimeDiscovery, Name: "runtime-discovery"},
		}
	case netbox.ClusterTypeRuntime:
		roles = []roleImport{
			{RoleID: netbox.RoleMetalRuntimeDiscovery, Name: "runtime-discovery"},
			{RoleID: netbox.RoleMetalComputeDiscovery, Name: "compute-discovery"},
		}
	}

	// Build DhcpPool objects from NetBox prefixes.
	var pools []dnsmasqv1beta1.DhcpPool

	for _, role := range roles {
		allPrefixes, err := nbClient.GetPrefixes(role.RoleID)
		if err != nil {
			fatalf("fetching prefixes for role %d (%s): %v", role.RoleID, role.Name, err)
		}

		// Filter prefixes to only those whose site belongs to the target region.
		// e.g. region "qa-de-1" matches sites "qa-de-1a", "qa-de-1b", "qa-de-1d".
		prefixes := netbox.FilterByRegion(allPrefixes, region)

		if len(prefixes) == 0 {
			fmt.Fprintf(os.Stderr, "warning: no prefixes found for role %d (%s) in region %q (checked %d total prefixes)\n",
				role.RoleID, role.Name, region, len(allPrefixes))
			continue
		}

		fmt.Fprintf(os.Stderr, "Found %d prefixes for %s in region %s (from %d total)\n",
			len(prefixes), role.Name, region, len(allPrefixes))

		var entries []dnsmasqv1beta1.DhcpPoolEntry
		for _, p := range prefixes {
			entry, err := netbox.PrefixToPoolEntry(p, cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: skipping prefix %s (id=%d): %v\n", p.Prefix, p.ID, err)
				continue
			}
			entries = append(entries, entry)
		}

		if len(entries) == 0 {
			continue
		}

		poolName := fmt.Sprintf("%s-%s", clusterName, role.Name)
		pool := dnsmasqv1beta1.DhcpPool{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "dnsmasq.kvaps.cf/v1beta1",
				Kind:       "DhcpPool",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      poolName,
				Namespace: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "netbox-importer",
					"metal.cloud.sap/cluster":      clusterName,
					"metal.cloud.sap/role":         role.Name,
				},
			},
			Spec: dnsmasqv1beta1.DhcpPoolSpec{
				Controller: controller,
				Pools:      entries,
			},
		}

		pools = append(pools, pool)
	}

	if len(pools) == 0 {
		fatalf("no pools generated — check site filter and NetBox data")
	}

	// Dry-run: just print and exit.
	if dryRun {
		for i, pool := range pools {
			if i > 0 {
				fmt.Println("---")
			}
			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(pool); err != nil {
					fatalf("encoding JSON: %v", err)
				}
			} else {
				printYAML(pool)
			}
		}
		fmt.Fprintf(os.Stderr, "\nDry-run: %d DhcpPool resource(s) printed. No changes applied.\n", len(pools))
		return
	}

	// Auto-apply: create or update DhcpPool CRs in the cluster.
	k8sClient, err := buildK8sClient()
	if err != nil {
		fatalf("building Kubernetes client: %v", err)
	}

	ctx := context.Background()

	for _, pool := range pools {
		if err := applyDhcpPool(ctx, k8sClient, &pool); err != nil {
			fatalf("applying DhcpPool %s/%s: %v", pool.Namespace, pool.Name, err)
		}
		fmt.Fprintf(os.Stderr, "Applied DhcpPool %s/%s (%d entries from NetBox, add-only merge)\n", pool.Namespace, pool.Name, len(pool.Spec.Pools))
	}

	fmt.Fprintf(os.Stderr, "\nSuccessfully applied %d DhcpPool resource(s)\n", len(pools))
}

// buildK8sClient creates a controller-runtime client using kubeconfig or in-cluster config.
func buildK8sClient() (client.Client, error) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = dnsmasqv1beta1.AddToScheme(scheme)

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %v", err)
	}

	c, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating client: %v", err)
	}

	return c, nil
}

// applyDhcpPool creates a DhcpPool CR or merges new entries into an existing one.
// This is add-only: new prefixes from NetBox are added, but existing entries that
// are no longer in NetBox are kept (not deleted).
func applyDhcpPool(ctx context.Context, c client.Client, desired *dnsmasqv1beta1.DhcpPool) error {
	existing := &dnsmasqv1beta1.DhcpPool{}
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}

	err := c.Get(ctx, key, existing)
	if errors.IsNotFound(err) {
		// Create new resource.
		return c.Create(ctx, desired)
	}
	if err != nil {
		return fmt.Errorf("getting existing DhcpPool: %v", err)
	}

	// Merge: add new entries, update existing ones (matched by rangeStart), never remove.
	merged := mergePoolEntries(existing.Spec.Pools, desired.Spec.Pools)
	existing.Labels = desired.Labels
	existing.Spec.Controller = desired.Spec.Controller
	existing.Spec.Pools = merged
	return c.Update(ctx, existing)
}

// mergePoolEntries merges new entries into existing ones (add-only).
// Entries are matched by RangeStart. New entries are added, matching entries
// are updated, but existing entries not present in newEntries are kept.
func mergePoolEntries(existing, newEntries []dnsmasqv1beta1.DhcpPoolEntry) []dnsmasqv1beta1.DhcpPoolEntry {
	// Index existing entries by RangeStart for quick lookup.
	byRange := make(map[string]int, len(existing))
	for i, e := range existing {
		byRange[e.RangeStart] = i
	}

	merged := make([]dnsmasqv1beta1.DhcpPoolEntry, len(existing))
	copy(merged, existing)

	for _, entry := range newEntries {
		if idx, found := byRange[entry.RangeStart]; found {
			// Update existing entry in place.
			merged[idx] = entry
		} else {
			// Add new entry.
			merged = append(merged, entry)
			byRange[entry.RangeStart] = len(merged) - 1
		}
	}

	return merged
}

// printYAML outputs a DhcpPool as hand-crafted YAML (no external YAML dependency).
func printYAML(pool dnsmasqv1beta1.DhcpPool) {
	fmt.Printf("apiVersion: %s\n", pool.TypeMeta.APIVersion)
	fmt.Printf("kind: %s\n", pool.TypeMeta.Kind)
	fmt.Println("metadata:")
	fmt.Printf("  name: %s\n", pool.ObjectMeta.Name)
	fmt.Printf("  namespace: %s\n", pool.ObjectMeta.Namespace)
	if len(pool.ObjectMeta.Labels) > 0 {
		fmt.Println("  labels:")
		for k, v := range pool.ObjectMeta.Labels {
			fmt.Printf("    %s: %q\n", k, v)
		}
	}
	fmt.Println("spec:")
	if pool.Spec.Controller != "" {
		fmt.Printf("  controller: %q\n", pool.Spec.Controller)
	}
	fmt.Println("  pools:")
	for _, e := range pool.Spec.Pools {
		fmt.Printf("    - rangeStart: %q\n", e.RangeStart)
		fmt.Printf("      rangeEnd: %q\n", e.RangeEnd)
		printOptionalField("      ", "netmask", e.Netmask)
		printOptionalField("      ", "broadcast", e.Broadcast)
		printOptionalField("      ", "gateway", e.Gateway)
		printOptionalField("      ", "leaseTime", e.LeaseTime)
		printOptionalField("      ", "tag", e.Tag)
		printOptionalField("      ", "dhcpBoot", e.DhcpBoot)
	}
}

func printOptionalField(indent, key, value string) {
	if value != "" {
		fmt.Printf("%s%s: %q\n", indent, key, value)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

// sanitize replaces characters that are not valid in Kubernetes resource names.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return '-'
	}, s)
}
