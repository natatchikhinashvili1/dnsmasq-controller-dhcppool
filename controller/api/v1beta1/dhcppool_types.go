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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DhcpPoolEntry defines a single DHCP address range for dnsmasq.
type DhcpPoolEntry struct {
	RangeStart string `json:"rangeStart"`
	RangeEnd   string `json:"rangeEnd"`
	LeaseTime  string `json:"leaseTime,omitempty"`
	Netmask    string `json:"netmask,omitempty"`
	Broadcast  string `json:"broadcast,omitempty"`
	Tag        string `json:"tag,omitempty"`
}

// DhcpPoolSpec defines the desired state of DhcpPool
type DhcpPoolSpec struct {
	Controller string          `json:"controller,omitempty"`
	Pools      []DhcpPoolEntry `json:"pools,omitempty"`
}

// DhcpPoolStatus defines the observed state of DhcpPool
type DhcpPoolStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Controller",type="string",JSONPath=".spec.controller"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// DhcpPool is the Schema for the dhcppools API
type DhcpPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DhcpPoolSpec   `json:"spec,omitempty"`
	Status DhcpPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DhcpPoolList contains a list of DhcpPool
type DhcpPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DhcpPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DhcpPool{}, &DhcpPoolList{})
}
