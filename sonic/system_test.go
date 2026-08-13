package sonic

import (
	"slices"
	"testing"
)

func TestParseContainers(t *testing.T) {
	for _, platform := range platforms {
		containers, err := parseContainers(fixture(t, platform, "docker_ps.jsonl"))
		if err != nil {
			t.Fatalf("%s: %s", platform, err)
		}

		names := []string{}
		for _, container := range containers {
			names = append(names, container.Name)

			if container.State == "" || container.Status == "" || container.Image == "" {
				t.Errorf("%s: incomplete container: %+v", platform, container)
			}
		}
		slices.Sort(names)

		want := []string{"bgp", "dhcp_relay", "snmp", "swss"}
		if !slices.Equal(names, want) {
			t.Errorf("%s: wrong containers, want: %v, got: %v", platform, want, names)
		}
	}
}

func TestCountRelayProcesses(t *testing.T) {
	// the fixtures also hold supervisord, rsyslogd, python3 and dhcpmon processes
	for _, platform := range platforms {
		if count := countRelayProcesses(fixture(t, platform, "docker_top_dhcp_relay.txt")); count != 6 {
			t.Errorf("%s: wrong relay count, want: 6, got: %d", platform, count)
		}
	}

	if count := countRelayProcesses("PID TTY TIME CMD\n1 pts/0 00:00:00 supervisord\n"); count != 0 {
		t.Errorf("a container without relay should report 0, got: %d", count)
	}
}

func TestParseIPTables(t *testing.T) {
	for _, platform := range platforms {
		filter := parseIPTables(fixture(t, platform, "iptables.txt"))

		for chain, want := range map[string]string{"INPUT": "ACCEPT", "FORWARD": "ACCEPT", "OUTPUT": "ACCEPT"} {
			if filter.Policies[chain] != want {
				t.Errorf("%s: wrong %s policy, want: %s, got: %s", platform, chain, want, filter.Policies[chain])
			}
		}

		// '-N SONIC_...' declarations must not become rules
		for _, rule := range filter.Rules {
			if rule.Chain == "" || rule.Target == "" {
				t.Errorf("%s: incomplete rule: %+v", platform, rule)
			}
		}

		if !hasRule(filter.Rules, IPTablesRule{Chain: "INPUT", Protocol: "tcp", DestPort: "179", Target: "ACCEPT"}) {
			t.Errorf("%s: BGP rule not found in %+v", platform, filter.Rules)
		}
		// a port range is kept as written
		if !hasRule(filter.Rules, IPTablesRule{Chain: "INPUT", Protocol: "udp", DestPort: "67:68", Target: "ACCEPT"}) {
			t.Errorf("%s: DHCP rule not found", platform)
		}
		if !hasRule(filter.Rules, IPTablesRule{Chain: "INPUT", Protocol: "tcp", Source: "198.18.0.0/16", DestPort: "22", Target: "ACCEPT"}) {
			t.Errorf("%s: SSH rule not found", platform)
		}

		last := filter.Rules[len(filter.Rules)-1]
		if last.Target != "DROP" || last.Raw != "-A INPUT -j DROP" {
			t.Errorf("%s: the last rule should drop everything, got: %+v", platform, last)
		}
	}
}

func TestParseIP6Tables(t *testing.T) {
	for _, platform := range platforms {
		filter := parseIPTables(fixture(t, platform, "ip6tables.txt"))

		if !hasRule(filter.Rules, IPTablesRule{Chain: "INPUT", Protocol: "ipv6-icmp", Target: "ACCEPT"}) {
			t.Errorf("%s: ICMPv6 rule not found in %+v", platform, filter.Rules)
		}
		if !hasRule(filter.Rules, IPTablesRule{Chain: "INPUT", Source: "::1/128", InInterface: "lo", Target: "ACCEPT"}) {
			t.Errorf("%s: loopback rule not found", platform)
		}
	}
}

// hasRule reports whether a rule matches every non empty field of want.
func hasRule(rules []IPTablesRule, want IPTablesRule) bool {
	return slices.ContainsFunc(rules, func(rule IPTablesRule) bool {
		rule.Raw = ""
		if want.Source == "" {
			rule.Source = ""
		}
		if want.InInterface == "" {
			rule.InInterface = ""
		}
		if want.Protocol == "" {
			rule.Protocol = ""
		}
		return rule == want
	})
}

func TestSortNames(t *testing.T) {
	tests := map[string][]string{
		"interfaces": {
			"Ethernet0", "Ethernet1", "Ethernet2", "Ethernet9", "Ethernet10", "Ethernet32", "Ethernet100",
			"PortChannel2", "PortChannel10", "Vlan506", "eth0",
		},
		// fan and sensor names of the fixtures, they have several numbers
		"fans of the w6510": {
			"Fantray1_1", "Fantray1_2", "Fantray2_1", "Fantray2_2", "Fantray10_1", "PSU1_FAN1", "PSU2_FAN1",
		},
		"sensors of the msn2700": {
			"Ambient Fan Side Temp", "CPU Core 0 Temp", "CPU Core 1 Temp", "CPU Core 10 Temp",
			"xSFP module 8 Temp", "xSFP module 9 Temp", "xSFP module 27 Temp",
		},
	}

	for name, want := range tests {
		names := slices.Clone(want)
		slices.Reverse(names)
		sortNames(names)

		if !slices.Equal(names, want) {
			t.Errorf("%s: wrong order,\nwant: %v\ngot:  %v", name, want, names)
		}
	}
}
