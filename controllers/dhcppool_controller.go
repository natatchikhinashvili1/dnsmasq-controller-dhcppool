package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dnsmasqv1beta1 "github.com/kvaps/dnsmasq-controller/api/v1beta1"
	"github.com/kvaps/dnsmasq-controller/pkg/conf"
	"github.com/kvaps/dnsmasq-controller/pkg/util"
)

// DhcpPoolReconciler reconciles a DhcpPool object
type DhcpPoolReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=dnsmasq.kvaps.cf,resources=dhcppools,verbs=get;list;watch
// +kubebuilder:rbac:groups=dnsmasq.kvaps.cf,resources=dhcppools/status,verbs=get;update;patch

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
	if err := r.Client.Get(ctx, req.NamespacedName, res); err != nil {
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

	poolCount := int32(len(res.Spec.Pools))

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
		return ctrl.Result{}, err
	}

	if configWritten {
		if err := util.TestConfig(tmpConfigFile); err != nil {
			_ = os.Remove(tmpConfigFile)
			log.Error(err, "Config "+tmpConfigFile+" is invalid!")
			r.updateStatus(ctx, res, false, poolCount, configFile, fmt.Sprintf("config validation failed: %v", err))
			return ctrl.Result{}, err
		}

		if err := os.Rename(tmpConfigFile, configFile); err != nil {
			_ = os.Remove(tmpConfigFile)
			log.Error(err, "Failed to move "+tmpConfigFile+" to "+configFile)
			r.updateStatus(ctx, res, false, poolCount, configFile, fmt.Sprintf("failed to move config: %v", err))
			return ctrl.Result{}, err
		}

		log.Info("Written " + configFile)
		config.Generation++
	}

	r.updateStatus(ctx, res, true, poolCount, configFile, "")
	return ctrl.Result{}, nil
}

func (r *DhcpPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dnsmasqv1beta1.DhcpPool{}).
		Complete(r)
}