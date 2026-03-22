package netbox

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"

	dnsmasqv1beta1 "github.com/kvaps/dnsmasq-controller/api/v1beta1"
)

// ClusterType distinguishes admin clusters from runtime clusters.
type ClusterType string

const (
	ClusterTypeAdmin   ClusterType = "admin"
	ClusterTypeRuntime ClusterType = "runtime"
)

// ImportConfig holds the parameters for converting NetBox prefixes to DhcpPool CRs.
type ImportConfig struct {
	ClusterType ClusterType
	ClusterName string // e.g. "a-qa-de-1" or "rt-qa-de-1"
	Region      string // e.g. "qa-de-1"
	Namespace   string // e.g. "metal-operator-dhcp"
	Controller  string // controller name for spec.controller
	LeaseTime   string // default lease time, e.g. "10m"
}

// DhcpBootURL returns the dhcp-boot URL based on cluster type and region.
func (c *ImportConfig) DhcpBootURL() string {
	switch c.ClusterType {
	case ClusterTypeAdmin:
		return fmt.Sprintf("https://boot-operator.admin.%s.cloud.sap/ipxe", c.Region)
	case ClusterTypeRuntime:
		return fmt.Sprintf("https://boot-operator-remote.runtime.%s.cloud.sap/ipxe", c.Region)
	default:
		return ""
	}
}

// PoolRangeFromCIDR calculates the DHCP pool range from a CIDR prefix.
// The pool starts at the 4th usable IP and ends at the last usable IP (broadcast - 1).
// Gateway is assumed to be the 1st usable IP (network + 1).
func PoolRangeFromCIDR(cidr string) (rangeStart, rangeEnd, gateway, netmask, broadcast string, err error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("parsing CIDR %q: %v", cidr, err)
	}

	ip = ip.To4()
	if ip == nil {
		return "", "", "", "", "", fmt.Errorf("only IPv4 is supported, got %q", cidr)
	}

	mask := ipNet.Mask
	ones, bits := mask.Size()
	if bits != 32 {
		return "", "", "", "", "", fmt.Errorf("unexpected mask size %d", bits)
	}

	// Network and broadcast addresses as uint32.
	networkInt := binary.BigEndian.Uint32(ipNet.IP.To4())
	hostBits := uint(32 - ones)
	broadcastInt := networkInt | ((1 << hostBits) - 1)

	// Need at least /29 (6 usable) to have a 4th usable IP.
	usableCount := broadcastInt - networkInt - 1 // exclude network and broadcast
	if usableCount < 4 {
		return "", "", "", "", "", fmt.Errorf("prefix %s too small: only %d usable IPs", cidr, usableCount)
	}

	gatewayInt := networkInt + 1       // 1st usable
	rangeStartInt := networkInt + 4    // 4th usable
	rangeEndInt := broadcastInt - 1    // last usable

	gateway = uint32ToIP(gatewayInt).String()
	rangeStart = uint32ToIP(rangeStartInt).String()
	rangeEnd = uint32ToIP(rangeEndInt).String()
	netmask = net.IP(mask).String()
	broadcast = uint32ToIP(broadcastInt).String()

	return rangeStart, rangeEnd, gateway, netmask, broadcast, nil
}

func uint32ToIP(n uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip
}

// PrefixToPoolEntry converts a NetBox prefix to a DhcpPoolEntry.
func PrefixToPoolEntry(prefix PrefixResult, cfg *ImportConfig) (dnsmasqv1beta1.DhcpPoolEntry, error) {
	rangeStart, rangeEnd, gateway, netmask, broadcast, err := PoolRangeFromCIDR(prefix.Prefix)
	if err != nil {
		return dnsmasqv1beta1.DhcpPoolEntry{}, err
	}

	tag := prefixTag(prefix)

	entry := dnsmasqv1beta1.DhcpPoolEntry{
		RangeStart: rangeStart,
		RangeEnd:   rangeEnd,
		Gateway:    gateway,
		Netmask:    netmask,
		Broadcast:  broadcast,
		LeaseTime:  cfg.LeaseTime,
		Tag:        tag,
		DhcpBoot:   cfg.DhcpBootURL(),
	}

	return entry, nil
}

// prefixTag generates a unique dnsmasq tag name from a prefix.
// Uses site slug + sanitized prefix to guarantee uniqueness, since a single site
// can have multiple prefixes (e.g. Compute Discovery has one /27 per building block).
// Example: site "qa-de-1b", prefix "10.245.248.32/27" → "qa-de-1b-10-245-248-32"
func prefixTag(p PrefixResult) string {
	// Strip the /mask from the CIDR and replace dots with dashes.
	cidr := p.Prefix
	if idx := strings.IndexByte(cidr, '/'); idx != -1 {
		cidr = cidr[:idx]
	}
	sanitized := strings.ReplaceAll(cidr, ".", "-")

	if p.Site != nil && p.Site.Slug != "" {
		return strings.ToLower(p.Site.Slug) + "-" + sanitized
	}
	return "net-" + sanitized
}
