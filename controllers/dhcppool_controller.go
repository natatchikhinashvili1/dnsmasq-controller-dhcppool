package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dnsmasqv1beta1 "github.com/kvaps/dnsmasq-controller/api/v1beta1"
	"github.com/kvaps/dnsmasq-controller/pkg/conf"
	"github.com/kvaps/dnsmasq-controller/pkg/netbox"
	"github.com/kvaps/dnsmasq-controller/pkg/util"
)

// DhcpPoolReconciler reconciles a DhcpPool object
type DhcpPoolReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=dnsmasq.kvaps.cf,resources=dhcppools,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=dnsmasq.kvaps.cf,resources=dhcppools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *DhcpPoolReconciler) updateStatus(
	ctx context.Context,
	res *dnsmasqv1beta1.DhcpPool,
	ready bool,
	poolCount int32,
	configFile string,
	errMsg string,
) {

	res.Status.Ready = ready
	res.Status.PoolCount = poolCount
	res.Status.ConfigFile = configFile
	res.Status.Error = errMsg

	if err := r.Client.Status().Update(ctx, res); err != nil {
		r.Log.Error(err, "Failed to update DhcpPool status")
	}
}

func (r *DhcpPoolReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("dhcppool", req.NamespacedName)

	config := conf.GetConfig()

	configFile := filepath.Join(config.DnsmasqConfDir, req.Namespace+"-"+req.Name+"-pool.conf")
	tmpConfigFile := filepath.Join(config.DnsmasqConfDir, "."+req.Namespace+"-"+req.Name+"-pool.conf.tmp")

	res := &dnsmasqv1beta1.DhcpPool{}
	if err := r.Get(ctx, req.NamespacedName, res); err != nil {
		if errors.IsNotFound(err) {
			if _, statErr := os.Stat(configFile); statErr == nil {
				if rmErr := os.Remove(configFile); rmErr != nil {
					log.Error(rmErr, "Failed to remove "+configFile)
					return ctrl.Result{}, rmErr
				}
				log.Info("Removed " + configFile)
				config.Generation++
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if res.Spec.Controller != config.ControllerName {
		if _, statErr := os.Stat(configFile); statErr == nil {
			if rmErr := os.Remove(configFile); rmErr != nil {
				log.Error(rmErr, "Failed to remove "+configFile)
				return ctrl.Result{}, rmErr
			}
			log.Info("Removed " + configFile)
			config.Generation++
		}
		return ctrl.Result{}, nil
	}

	// If NetBox import is configured, fetch and merge entries.
	var syncInterval time.Duration
	if res.Spec.NetBoxImport != nil {
		interval, importedCount, updated, err := r.reconcileNetBoxImport(ctx, log, res)
		syncInterval = interval
		if err != nil {
			log.Error(err, "NetBox import failed")
			res.Status.Error = fmt.Sprintf("netbox import: %v", err)
		} else {
			res.Status.ImportedCount = importedCount
			res.Status.LastNetBoxSync = time.Now().UTC().Format(time.RFC3339)
		}
		if updated {
			// The spec was updated, which will trigger another reconcile.
			// Return early — the next reconcile will write the dnsmasq config.
			return ctrl.Result{}, nil
		}
	}

	// Write dnsmasq config from all pools (manual + imported).
	poolCount := int32(len(res.Spec.Pools)) //#nosec G115 -- pool count is always small

	var b strings.Builder
	if poolCount > 0 {
		b.Grow(int(poolCount) * 64)
	}

	for _, p := range res.Spec.Pools {
		b.WriteString(p.ToDnsmasqConfig())
		b.WriteByte('\n')
	}
	configBytes := []byte(b.String())

	configWritten, err := util.WriteConfig(configFile, tmpConfigFile, configBytes)
	if err != nil {
		log.Error(err, "Failed to update "+configFile)
		r.updateStatus(ctx, res, false, poolCount, configFile, fmt.Sprintf("failed to write config: %v", err))
		return ctrl.Result{RequeueAfter: syncInterval}, err
	}

	if configWritten {
		if err := util.TestConfig(tmpConfigFile); err != nil {
			_ = os.Remove(tmpConfigFile)
			log.Error(err, "Config "+tmpConfigFile+" is invalid!")
			r.updateStatus(ctx, res, false, poolCount, configFile, fmt.Sprintf("config validation failed: %v", err))
			return ctrl.Result{RequeueAfter: syncInterval}, err
		}

		if err := os.Rename(tmpConfigFile, configFile); err != nil {
			_ = os.Remove(tmpConfigFile)
			log.Error(err, "Failed to move "+tmpConfigFile+" to "+configFile)
			r.updateStatus(ctx, res, false, poolCount, configFile, fmt.Sprintf("failed to move config: %v", err))
			return ctrl.Result{RequeueAfter: syncInterval}, err
		}

		log.Info("Written " + configFile)
		config.Generation++
	}

	r.updateStatus(ctx, res, true, poolCount, configFile, "")
	return ctrl.Result{RequeueAfter: syncInterval}, nil
}

// reconcileNetBoxImport fetches prefixes from NetBox and merges them into the DhcpPool.
// Returns the sync interval, number of imported entries, whether the CR was updated, and any error.
func (r *DhcpPoolReconciler) reconcileNetBoxImport(
	ctx context.Context,
	log logr.Logger,
	res *dnsmasqv1beta1.DhcpPool,
) (time.Duration, int32, bool, error) {
	imp := res.Spec.NetBoxImport

	// Parse sync interval.
	syncInterval := 5 * time.Minute
	if imp.SyncInterval != "" {
		parsed, err := time.ParseDuration(imp.SyncInterval)
		if err != nil {
			log.Error(err, "invalid syncInterval, using default 5m")
		} else {
			syncInterval = parsed
		}
	}

	// Read the NetBox token from the referenced Secret.
	token, err := r.getSecretValue(ctx, res.Namespace, imp.TokenSecretRef)
	if err != nil {
		return syncInterval, 0, false, fmt.Errorf("reading token secret: %v", err)
	}

	leaseTime := imp.LeaseTime
	if leaseTime == "" {
		leaseTime = "10m"
	}

	cfg := &netbox.ImportConfig{
		ClusterType: netbox.ClusterType(imp.ClusterType),
		ClusterName: imp.ClusterName,
		Region:      imp.Region,
		Namespace:   res.Namespace,
		Controller:  res.Spec.Controller,
		LeaseTime:   leaseTime,
	}

	nbClient := netbox.NewClient(imp.NetboxURL, token)

	var newEntries []dnsmasqv1beta1.DhcpPoolEntry

	for _, role := range imp.Roles {
		allPrefixes, err := nbClient.GetPrefixes(role.RoleID)
		if err != nil {
			return syncInterval, 0, false, fmt.Errorf("fetching prefixes for role %d (%s): %v", role.RoleID, role.Name, err)
		}

		prefixes := netbox.FilterByRegion(allPrefixes, imp.Region)
		log.Info("fetched prefixes from NetBox", "role", role.Name, "total", len(allPrefixes), "filtered", len(prefixes))

		for _, p := range prefixes {
			entry, err := netbox.PrefixToPoolEntry(p, cfg)
			if err != nil {
				log.Info("skipping prefix", "prefix", p.Prefix, "error", err)
				continue
			}
			newEntries = append(newEntries, entry)
		}
	}

	if len(newEntries) > 0 {
		// Add-only merge: keep existing entries, add/update from NetBox.
		merged := mergePoolEntries(res.Spec.Pools, newEntries)

		// Only update the CR if the merge actually changed something.
		if !poolEntriesEqual(res.Spec.Pools, merged) {
			res.Spec.Pools = merged
			if err := r.Client.Update(ctx, res); err != nil {
				return syncInterval, 0, false, fmt.Errorf("updating DhcpPool spec: %v", err)
			}
			log.Info("merged NetBox entries into pools", "newEntries", len(newEntries), "totalPools", len(merged))
			return syncInterval, int32(len(newEntries)), true, nil
		}
	}

	return syncInterval, int32(len(newEntries)), false, nil
}

// getSecretValue reads a value from a Kubernetes Secret.
func (r *DhcpPoolReconciler) getSecretValue(ctx context.Context, namespace string, ref dnsmasqv1beta1.SecretKeyRef) (string, error) {
	secret := &corev1.Secret{}
	key := types.NamespacedName{Name: ref.Name, Namespace: namespace}
	if err := r.Client.Get(ctx, key, secret); err != nil {
		return "", fmt.Errorf("getting secret %s: %v", key, err)
	}
	data, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s", ref.Key, key)
	}
	return string(data), nil
}

// mergePoolEntries merges new entries into existing ones (add-only).
// Entries are matched by RangeStart. New entries are added, matching entries
// are updated, but existing entries not present in newEntries are kept.
func mergePoolEntries(existing, newEntries []dnsmasqv1beta1.DhcpPoolEntry) []dnsmasqv1beta1.DhcpPoolEntry {
	byRange := make(map[string]int, len(existing))
	for i, e := range existing {
		byRange[e.RangeStart] = i
	}

	merged := make([]dnsmasqv1beta1.DhcpPoolEntry, len(existing))
	copy(merged, existing)

	for _, entry := range newEntries {
		if idx, found := byRange[entry.RangeStart]; found {
			merged[idx] = entry
		} else {
			merged = append(merged, entry)
			byRange[entry.RangeStart] = len(merged) - 1
		}
	}

	return merged
}

// poolEntriesEqual returns true if two pool entry slices have the same entries in the same order.
func poolEntriesEqual(a, b []dnsmasqv1beta1.DhcpPoolEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (r *DhcpPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dnsmasqv1beta1.DhcpPool{}).
		Complete(r)
}
