package sonic

import "testing"

func TestLLDPNeighbor(t *testing.T) {
	tests := map[string]struct {
		platform string
		intf     string
		want     Neighbor
		portName string
	}{
		"uplink to a spine": {
			platform: "msn2700",
			intf:     "Ethernet0",
			want: Neighbor{
				Host:            "sp01-15-p02-dc1-pnet.example.net",
				Port:            "Ethernet16",
				PortDescription: "LOCAL:ra01-15-p02-dc1-pnet.example.net:Ethernet0",
			},
			portName: "Ethernet16",
		},
		"management port to a juniper switch": {
			platform: "msn2700",
			intf:     "eth0",
			want: Neighbor{
				Host:            "ra01-15-p02-dc1-mnet.example.net",
				Port:            "ge-0/0/45",
				PortDescription: "LOCAL:ra01-15-p02-dc1-pnet.example.net:eth0",
			},
			portName: "ge-0/0/45",
		},
		// a server without an LLDP daemon: the chassis is inlined and identified by the MAC of
		// the NIC, the port by the MAC of its port, so the description is the only readable name
		"server without lldpd": {
			platform: "msn2700",
			intf:     "Ethernet8",
			want: Neighbor{
				Host:            "00:00:5e:00:53:04",
				Port:            "00:00:5e:00:53:05",
				PortDescription: "ConnectX-4 Lx, 25G/10G/1G SFP",
			},
			portName: "ConnectX-4 Lx, 25G/10G/1G SFP",
		},
		"other platform uplink": {
			platform: "w6510",
			intf:     "Ethernet96",
			want: Neighbor{
				Host:            "sp04-15-p02-dc1-pnet.example.net",
				Port:            "Ethernet16",
				PortDescription: "LOCAL:rb01-15-p02-dc1-pnet.example.net:Ethernet96",
			},
			portName: "Ethernet16",
		},
		"other platform server": {
			platform: "w6510",
			intf:     "Ethernet17",
			want: Neighbor{
				Host:            "00:00:5e:00:53:0f",
				Port:            "00:00:5e:00:53:10",
				PortDescription: "ConnectX-4 Lx, 25G/10G/1G SFP",
			},
			portName: "ConnectX-4 Lx, 25G/10G/1G SFP",
		},
		"interface without neighbor": {
			platform: "msn2700",
			intf:     "Ethernet64",
		},
	}

	for name, test := range tests {
		var lldp LLDP
		fixtureJSON(t, test.platform, "lldpctl.json", &lldp)

		got := lldp.Neighbor(test.intf)
		if got != test.want {
			t.Errorf("%s: wrong neighbor,\nwant: %+v\ngot:  %+v", name, test.want, got)
		}
		if got.PortName() != test.portName {
			t.Errorf("%s: wrong port name, want: %q, got: %q", name, test.portName, got.PortName())
		}
	}
}

// the LLDP data of a spine tells which local interface it sees, an uplink cabled on the wrong
// port is detected by comparing it with the local interface.
func TestLLDPNeighborRemoteDescription(t *testing.T) {
	var lldp LLDP
	fixtureJSON(t, "msn2700", "lldpctl.json", &lldp)

	neighbor := lldp.Neighbor("Ethernet32")
	if neighbor.PortDescription != "LOCAL:rb01-15-p02-dc1-pnet.example.net:Ethernet64" {
		t.Fatalf("wrong port description: %q", neighbor.PortDescription)
	}
}
