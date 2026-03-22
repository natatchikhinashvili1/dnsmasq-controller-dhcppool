package netbox

import (
	"testing"
)

func TestPoolRangeFromCIDR(t *testing.T) {
	tests := []struct {
		cidr       string
		wantStart  string
		wantEnd    string
		wantGW     string
		wantMask   string
		wantBcast  string
		wantErr    bool
	}{
		{
			cidr:      "10.0.0.0/27",
			wantStart: "10.0.0.4",
			wantEnd:   "10.0.0.30",
			wantGW:    "10.0.0.1",
			wantMask:  "255.255.255.224",
			wantBcast: "10.0.0.31",
		},
		{
			cidr:      "10.0.0.0/24",
			wantStart: "10.0.0.4",
			wantEnd:   "10.0.0.254",
			wantGW:    "10.0.0.1",
			wantMask:  "255.255.255.0",
			wantBcast: "10.0.0.255",
		},
		{
			cidr:      "192.168.1.0/28",
			wantStart: "192.168.1.4",
			wantEnd:   "192.168.1.14",
			wantGW:    "192.168.1.1",
			wantMask:  "255.255.255.240",
			wantBcast: "192.168.1.15",
		},
		{
			// /30 has only 2 usable IPs — too small for 4th usable
			cidr:    "10.0.0.0/30",
			wantErr: true,
		},
		{
			// /29 has 6 usable IPs — just enough
			cidr:      "10.0.0.0/29",
			wantStart: "10.0.0.4",
			wantEnd:   "10.0.0.6",
			wantGW:    "10.0.0.1",
			wantMask:  "255.255.255.248",
			wantBcast: "10.0.0.7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.cidr, func(t *testing.T) {
			start, end, gw, mask, bcast, err := PoolRangeFromCIDR(tt.cidr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %s, got none", tt.cidr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if start != tt.wantStart {
				t.Errorf("rangeStart: got %s, want %s", start, tt.wantStart)
			}
			if end != tt.wantEnd {
				t.Errorf("rangeEnd: got %s, want %s", end, tt.wantEnd)
			}
			if gw != tt.wantGW {
				t.Errorf("gateway: got %s, want %s", gw, tt.wantGW)
			}
			if mask != tt.wantMask {
				t.Errorf("netmask: got %s, want %s", mask, tt.wantMask)
			}
			if bcast != tt.wantBcast {
				t.Errorf("broadcast: got %s, want %s", bcast, tt.wantBcast)
			}
		})
	}
}

func TestFilterByRegion(t *testing.T) {
	prefixes := []PrefixResult{
		{ID: 1, Prefix: "10.219.55.0/27", Site: &struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		}{ID: 1, Name: "QA-DE-1a", Slug: "qa-de-1a"}},
		{ID: 2, Prefix: "10.219.55.32/27", Site: &struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		}{ID: 2, Name: "QA-DE-1b", Slug: "qa-de-1b"}},
		{ID: 3, Prefix: "10.219.55.64/27", Site: &struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		}{ID: 3, Name: "QA-DE-1d", Slug: "qa-de-1d"}},
		{ID: 4, Prefix: "10.219.55.96/27", Site: &struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		}{ID: 4, Name: "EU-DE-1a", Slug: "eu-de-1a"}},
		{ID: 5, Prefix: "10.44.9.64/27", Site: &struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		}{ID: 5, Name: "qa-de-8a", Slug: "qa-de-8a"}},
		{ID: 6, Prefix: "10.0.0.0/27"}, // no site — should be excluded
	}

	// Filter for region "qa-de-1" should match qa-de-1a, qa-de-1b, qa-de-1d
	got := FilterByRegion(prefixes, "qa-de-1")
	if len(got) != 3 {
		t.Fatalf("expected 3 prefixes for qa-de-1, got %d", len(got))
	}
	for _, p := range got {
		if p.Site.Slug != "qa-de-1a" && p.Site.Slug != "qa-de-1b" && p.Site.Slug != "qa-de-1d" {
			t.Errorf("unexpected site %q in results", p.Site.Slug)
		}
	}

	// Filter for region "eu-de-1" should match only eu-de-1a
	got = FilterByRegion(prefixes, "eu-de-1")
	if len(got) != 1 {
		t.Fatalf("expected 1 prefix for eu-de-1, got %d", len(got))
	}
	if got[0].Site.Slug != "eu-de-1a" {
		t.Errorf("expected eu-de-1a, got %q", got[0].Site.Slug)
	}

	// Filter for region "qa-de-8" should match qa-de-8a
	got = FilterByRegion(prefixes, "qa-de-8")
	if len(got) != 1 {
		t.Fatalf("expected 1 prefix for qa-de-8, got %d", len(got))
	}

	// Filter for region "na-us-1" should match nothing
	got = FilterByRegion(prefixes, "na-us-1")
	if len(got) != 0 {
		t.Fatalf("expected 0 prefixes for na-us-1, got %d", len(got))
	}
}

func TestDhcpBootURL(t *testing.T) {
	tests := []struct {
		clusterType ClusterType
		region      string
		want        string
	}{
		{
			clusterType: ClusterTypeAdmin,
			region:      "qa-de-1",
			want:        "https://boot-operator.admin.qa-de-1.cloud.sap/ipxe",
		},
		{
			clusterType: ClusterTypeRuntime,
			region:      "qa-de-1",
			want:        "https://boot-operator-remote.runtime.qa-de-1.cloud.sap/ipxe",
		},
	}

	for _, tt := range tests {
		cfg := &ImportConfig{
			ClusterType: tt.clusterType,
			Region:      tt.region,
		}
		got := cfg.DhcpBootURL()
		if got != tt.want {
			t.Errorf("DhcpBootURL(%s, %s): got %s, want %s", tt.clusterType, tt.region, got, tt.want)
		}
	}
}
