package sonic

import (
	"slices"
	"testing"
)

func TestSortPeers(t *testing.T) {
	tests := map[string]struct {
		peers []string
		want  []string
	}{
		"addresses sort on their value": {
			peers: []string{"192.0.2.10", "192.0.2.9", "192.0.2.2"},
			want:  []string{"192.0.2.2", "192.0.2.9", "192.0.2.10"},
		},
		"IPv4 before IPv6": {
			peers: []string{"2001:db8::1", "203.0.113.1", "2001:db8::2", "10.0.0.1"},
			want:  []string{"10.0.0.1", "203.0.113.1", "2001:db8::1", "2001:db8::2"},
		},
		"IPv6 sorts on its value too": {
			peers: []string{"2001:db8::10", "2001:db8::2", "2001:db8:0:fe::1"},
			want:  []string{"2001:db8::2", "2001:db8::10", "2001:db8:0:fe::1"},
		},
		"unnumbered peers come last, sorted as names": {
			peers: []string{"Ethernet10", "192.0.2.1", "Ethernet2", "2001:db8::1"},
			want:  []string{"192.0.2.1", "2001:db8::1", "Ethernet2", "Ethernet10"},
		},
	}

	for name, test := range tests {
		SortPeers(test.peers)

		if !slices.Equal(test.peers, test.want) {
			t.Errorf("%s: want: %v, got: %v", name, test.want, test.peers)
		}
	}
}
