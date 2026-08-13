package sonic

import (
	"net/netip"
	"slices"
	"testing"
)

func TestExtractRoutes(t *testing.T) {
	tests := map[string]struct {
		testFile string
		want     []Route
	}{
		"simple": {
			testFile: "./test_data/showiproute.txt",
			want: []Route{
				{"", "Ethernet224", true},
			},
		},
		"multiple": {
			testFile: "./test_data/showiproute_multiple.txt",
			want: []Route{
				{"10.1.0.225", "Ethernet0", false},
				{"10.1.0.227", "Ethernet8", false},
				{"10.1.0.229", "Ethernet64", false},
				{"10.1.0.231", "Ethernet72", false},
				{"10.1.0.233", "Ethernet128", false},
				{"10.1.0.235", "Ethernet136", false},
				{"10.1.0.237", "Ethernet192", false},
				{"10.1.0.239", "Ethernet200", false},
			},
		},
	}

	for name, test := range tests {
		var routeTable RouteTable
		loadJSON(t, test.testFile, &routeTable)

		if routes := ExtractRoutes(routeTable); !slices.Equal(routes, test.want) {
			t.Fatalf("%s: wrong routes, want: %v, got: %v", name, test.want, routes)
		}
	}
}

func TestOtherP2PHost(t *testing.T) {
	tests := map[string]struct {
		wantedPrefix netip.Prefix
		wantError    bool
	}{
		"10.0.0.0/31": {wantedPrefix: netip.MustParsePrefix("10.0.0.1/31")},
		"10.0.0.1/31": {wantedPrefix: netip.MustParsePrefix("10.0.0.0/31")},
		"10.0.0.2/31": {wantedPrefix: netip.MustParsePrefix("10.0.0.3/31")},
		"10.0.0.1/30": {wantedPrefix: netip.MustParsePrefix("10.0.0.2/30")},
		"10.0.0.2/30": {wantedPrefix: netip.MustParsePrefix("10.0.0.1/30")},
		"10.0.0.0/30": {wantError: true},
		"10.0.0.3/30": {wantError: true},
		"::/127":      {wantedPrefix: netip.MustParsePrefix("::1/127")},
		"::1/127":     {wantedPrefix: netip.MustParsePrefix("::/127")},
		"::1/64":      {wantError: true},
	}

	for cidr, test := range tests {
		other, err := OtherP2PHost(netip.MustParsePrefix(cidr))

		if (err != nil) != test.wantError {
			t.Fatalf("%s: error mismatch, want error: %t, got: %v", cidr, test.wantError, err)
		}

		if other != test.wantedPrefix {
			t.Fatalf("%s: mismatch, want: %s, got: %s", cidr, test.wantedPrefix, other)
		}
	}
}
