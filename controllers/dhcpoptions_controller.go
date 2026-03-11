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

// DhcpOptionsReconciler reconciles a DhcpOptions object
type DhcpOptionsReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=dnsmasq.kvaps.cf,resources=dhcpoptions,verbs=get;list;watch

func (r *DhcpOptionsReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("dnsmasqdhcpoptionset", req.NamespacedName)
	config := conf.GetConfig()

	configFile := config.DnsmasqConfDir + "/dhcp-opts/" + req.Namespace + "-" + req.Name

	res := &dnsmasqv1beta1.DhcpOptions{}
	err := r.Get(context.TODO(), req.NamespacedName, res)
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

	// Write dhcp-options
	var configData strings.Builder
	for _, o := range res.Spec.Options {
		var configLine strings.Builder
		for _, v := range o.Tags {
			configLine.WriteString(",tag:")
			configLine.WriteString(v)
		}
		if o.Encap != "" {
			configLine.WriteString(",encap:")
			configLine.WriteString(o.Encap)
		}
		if o.ViEncap != "" {
			configLine.WriteString(",vi-encap:")
			configLine.WriteString(o.ViEncap)
		}
		if o.Vendor != "" {
			configLine.WriteString(",vendor:")
			configLine.WriteString(o.Vendor)
		}
		if o.Key != "" {
			configLine.WriteString(",")
			configLine.WriteString(o.Key)
		}
		for _, v := range o.Values {
			configLine.WriteString(",")
			configLine.WriteString(v)
		}
		configLine.WriteString("\n")
		line := configLine.String()
		configData.WriteString(line[1:])
	}
	configBytes := []byte(configData.String())

	configWritten, err := util.WriteConfig(configFile, configFile, configBytes)
	if err != nil {
		r.Log.Error(err, "Failed to update "+configFile)
		return ctrl.Result{}, nil
	}

	if configWritten {
		r.Log.Info("Written " + configFile)
		config.Generation++
	}

	return ctrl.Result{}, nil
}

func (r *DhcpOptionsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dnsmasqv1beta1.DhcpOptions{}).
		Complete(r)
}
