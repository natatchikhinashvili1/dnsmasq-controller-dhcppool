/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"os"

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

func (r *DhcpPoolReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("dhcppool", req.NamespacedName)
	config := conf.GetConfig()

	configFile := config.DnsmasqConfDir + "/" + req.Namespace + "-" + req.Name + "-pool.conf"
	tmpConfigFile := config.DnsmasqConfDir + "/." + req.Namespace + "-" + req.Name + "-pool.conf.tmp"

	res := &dnsmasqv1beta1.DhcpPool{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, res)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found
			if _, err := os.Stat(configFile); !os.IsNotExist(err) {
				os.Remove(configFile)
				r.Log.Info("Removed " + configFile)
				config.Generation++
			}
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	if res.Spec.Controller != config.ControllerName {
		if _, err := os.Stat(configFile); !os.IsNotExist(err) {
			// Controller name has been changed
			os.Remove(configFile)
			r.Log.Info("Removed " + configFile)
			config.Generation++
		}
		return ctrl.Result{}, nil
	}

	// Write dhcp-range lines
	var configData string
	for _, p := range res.Spec.Pools {
		configLine := "dhcp-range="
		if p.Tag != "" {
			configLine += "set:" + p.Tag + ","
		}
		configLine += p.RangeStart + "," + p.RangeEnd
		if p.Netmask != "" {
			configLine += "," + p.Netmask
		}
		if p.Broadcast != "" {
			configLine += "," + p.Broadcast
		}
		if p.LeaseTime != "" {
			configLine += "," + p.LeaseTime
		}
		configData += configLine + "\n"
	}
	configBytes := []byte(configData)

	configWritten, err := util.WriteConfig(configFile, tmpConfigFile, configBytes)
	if err != nil {
		r.Log.Error(err, "Failed to update "+configFile)
		return ctrl.Result{}, nil
	}

	if configWritten {
		if err = util.TestConfig(tmpConfigFile); err != nil {
			r.Log.Error(err, "Config "+tmpConfigFile+" is invalid!")
			return ctrl.Result{}, nil
		}

		if err = os.Rename(tmpConfigFile, configFile); err != nil {
			os.Remove(tmpConfigFile)
			r.Log.Error(err, "Failed to move "+tmpConfigFile+" to "+configFile)
			return ctrl.Result{}, nil
		}
		r.Log.Info("Written " + configFile)
		config.Generation++
	}

	return ctrl.Result{}, nil
}

func (r *DhcpPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dnsmasqv1beta1.DhcpPool{}).
		Complete(r)
}
