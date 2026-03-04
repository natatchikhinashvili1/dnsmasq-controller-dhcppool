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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// dnsmasq config directive prefixes.
const (
	dnsmasqDhcpRange  = "dhcp-range="
	dnsmasqDhcpOption = "dhcp-option="
	dnsmasqDhcpBoot   = "dhcp-boot="
	dnsmasqTagPrefix  = "tag:"
	dnsmasqSetPrefix  = "set:"
	dnsmasqOptRouter  = "option:router,"
)

// DhcpPoolEntry defines a single DHCP address range for dnsmasq.
type DhcpPoolEntry struct {
	// +kubebuilder:validation:Pattern=`^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`
	RangeStart string `json:"rangeStart"`
	// +kubebuilder:validation:Pattern=`^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`
	RangeEnd string `json:"rangeEnd"`
	// +kubebuilder:validation:Pattern=`^(\d+[smhd]|infinite)$`
	LeaseTime string `json:"leaseTime,omitempty"`
	// +kubebuilder:validation:Pattern=`^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`
	Netmask string `json:"netmask,omitempty"`
	// +kubebuilder:validation:Pattern=`^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`
	Broadcast string `json:"broadcast,omitempty"`
	// +kubebuilder:validation:Pattern=`^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`
	Gateway  string `json:"gateway,omitempty"`
	// Raw dhcp-boot value for PXE booting (e.g. "https://boot.example.com/ipxe"); the tag: prefix is added automatically when Tag is set.
	DhcpBoot string `json:"dhcpBoot,omitempty"`
	// Tag to associate with this DHCP range, used to scope dhcp-option and dhcp-boot directives.
	Tag string `json:"tag,omitempty"`
}

// writeTagPrefix writes "tag:<tag>," to b if the pool has a tag set.
func (p DhcpPoolEntry) writeTagPrefix(b *strings.Builder) {
	if p.Tag != "" {
		b.WriteString(dnsmasqTagPrefix)
		b.WriteString(p.Tag)
		b.WriteByte(',')
	}
}

// ToDnsmasqConfig renders the entry as dnsmasq config lines (dhcp-range and dhcp-option).
func (p DhcpPoolEntry) ToDnsmasqConfig() string {
	var b strings.Builder
	b.WriteString(dnsmasqDhcpRange)
	if p.Tag != "" {
		b.WriteString(dnsmasqSetPrefix)
		b.WriteString(p.Tag)
		b.WriteByte(',')
	}
	b.WriteString(p.RangeStart)
	b.WriteByte(',')
	b.WriteString(p.RangeEnd)
	if p.Netmask != "" {
		b.WriteByte(',')
		b.WriteString(p.Netmask)
	}
	if p.Broadcast != "" {
		b.WriteByte(',')
		b.WriteString(p.Broadcast)
	}
	if p.LeaseTime != "" {
		b.WriteByte(',')
		b.WriteString(p.LeaseTime)
	}
	if p.Gateway != "" {
		b.WriteByte('\n')
		b.WriteString(dnsmasqDhcpOption)
		p.writeTagPrefix(&b)
		b.WriteString(dnsmasqOptRouter)
		b.WriteString(p.Gateway)
	}
	if p.DhcpBoot != "" {
		b.WriteByte('\n')
		b.WriteString(dnsmasqDhcpBoot)
		p.writeTagPrefix(&b)
		b.WriteString(p.DhcpBoot)
	}
	return b.String()
}

// DhcpPoolSpec defines the desired state of DhcpPool
type DhcpPoolSpec struct {
	Controller string          `json:"controller,omitempty"`
	Pools      []DhcpPoolEntry `json:"pools,omitempty"`
}

// DhcpPoolStatus defines the observed state of DhcpPool
type DhcpPoolStatus struct {
	Ready      bool   `json:"ready"`
	PoolCount  int32  `json:"poolCount"`
	ConfigFile string `json:"configFile,omitempty"`
	Error      string `json:"error,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Controller",type="string",JSONPath=".spec.controller"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="PoolCount",type="integer",JSONPath=".status.poolCount"
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
