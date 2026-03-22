package netbox

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// RoleID constants for NetBox prefix roles.
const (
	RoleMetalRuntimeDiscovery = 40
	RoleMetalComputeDiscovery = 38
)

// Client talks to the NetBox REST API.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewClient creates a NetBox API client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL:    baseURL,
		Token:      token,
		HTTPClient: http.DefaultClient,
	}
}

// PrefixResult is a single prefix returned by the NetBox API.
type PrefixResult struct {
	ID     int    `json:"id"`
	Prefix string `json:"prefix"` // CIDR notation, e.g. "10.0.0.0/27"
	Site   *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"site"`
	Role *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"role"`
	Status *struct {
		Value string `json:"value"`
	} `json:"status"`
	VLAN *struct {
		ID          int    `json:"id"`
		VID         int    `json:"vid"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	} `json:"vlan"`
	Description string `json:"description"`
}

// PrefixListResponse is the paginated response from /api/ipam/prefixes/.
type PrefixListResponse struct {
	Count    int            `json:"count"`
	Next     *string        `json:"next"`
	Previous *string        `json:"previous"`
	Results  []PrefixResult `json:"results"`
}

// GetPrefixes fetches all prefixes for a given role ID.
// It handles pagination automatically.
func (c *Client) GetPrefixes(roleID int) ([]PrefixResult, error) {
	var all []PrefixResult

	params := url.Values{}
	params.Set("role_id", strconv.Itoa(roleID))
	params.Set("status", "active")
	params.Set("limit", "100")

	nextURL := fmt.Sprintf("%s/api/ipam/prefixes/?%s", c.BaseURL, params.Encode())

	for nextURL != "" {
		resp, err := c.doGet(nextURL)
		if err != nil {
			return nil, err
		}

		all = append(all, resp.Results...)

		if resp.Next != nil {
			nextURL = *resp.Next
		} else {
			nextURL = ""
		}
	}

	return all, nil
}

// FilterByRegion returns only prefixes whose site slug starts with the given region.
// For example, region "qa-de-1" matches sites "qa-de-1a", "qa-de-1b", "qa-de-1d".
// Comparison is case-insensitive.
func FilterByRegion(prefixes []PrefixResult, region string) []PrefixResult {
	regionLower := strings.ToLower(region)
	var filtered []PrefixResult
	for _, p := range prefixes {
		if p.Site == nil {
			continue
		}
		siteSlug := strings.ToLower(p.Site.Slug)
		if strings.HasPrefix(siteSlug, regionLower) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func (c *Client) doGet(rawURL string) (*PrefixListResponse, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %v", err)
	}
	req.Header.Set("Authorization", "Token "+c.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("NetBox API returned %d: %s", resp.StatusCode, string(body))
	}

	var result PrefixListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %v", err)
	}

	return &result, nil
}
