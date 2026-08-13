package sonic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SysctlKeys are the kernel settings required by the monitoring and probing tools.
var SysctlKeys = []string{
	"net.ipv4.icmp_ratemask",
	"net.ipv6.icmp.ratemask",
	"net.ipv4.ip_local_reserved_ports",
}

// Sysctls returns the value of the given sysctl keys, keys absent on the device are not returned.
func Sysctls(keys []string) map[string]string {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		data, err := os.ReadFile("/proc/sys/" + strings.ReplaceAll(key, ".", "/"))
		if err != nil {
			continue
		}
		values[key] = strings.TrimSpace(string(data))
	}
	return values
}

// Container is a docker container of the device.
type Container struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	State  string `json:"state"`
	Status string `json:"status"`
}

// dockerContainer is a Container with the field names of 'docker ps'.
type dockerContainer struct {
	Name   string `json:"Names"`
	Image  string `json:"Image"`
	State  string `json:"State"`
	Status string `json:"Status"`
}

func Containers(ctx context.Context) ([]Container, error) {
	out, err := run(ctx, "docker", "ps", "--all", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	return parseContainers(out)
}

// parseContainers reads the one JSON object per container printed by 'docker ps'.
func parseContainers(out string) ([]Container, error) {
	containers := []Container{}

	decoder := json.NewDecoder(strings.NewReader(out))
	for decoder.More() {
		container := dockerContainer{}
		if err := decoder.Decode(&container); err != nil {
			return nil, fmt.Errorf("failed to parse docker output: %w", err)
		}
		containers = append(containers, Container(container))
	}

	return containers, nil
}

// DHCPRelayProcesses returns the number of relay processes running in the dhcp_relay container.
// There is one dhcrelay process per relayed VLAN, the manager and monitor processes are not counted.
func DHCPRelayProcesses(ctx context.Context) (int, error) {
	out, err := run(ctx, "docker", "top", "dhcp_relay", "-e")
	if err != nil {
		return 0, err
	}
	return countRelayProcesses(out), nil
}

// countRelayProcesses reads the process list printed by 'docker top -e': PID TTY TIME CMD.
func countRelayProcesses(out string) int {
	count := 0

	for _, line := range lines(out) {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		switch filepath.Base(fields[3]) {
		case "dhcrelay", "dhcp6relay":
			count++
		}
	}

	return count
}

type IPTables struct {
	IPv4 IPTablesFilter `json:"ipv4"`
	IPv6 IPTablesFilter `json:"ipv6"`
}

// IPTablesFilter is the filter table of one address family, as reported by 'iptables -S'.
type IPTablesFilter struct {
	Policies map[string]string `json:"policies"`
	Rules    []IPTablesRule    `json:"rules"`
}

type IPTablesRule struct {
	Chain        string `json:"chain"`
	Protocol     string `json:"protocol"`
	Source       string `json:"source"`
	Destination  string `json:"destination"`
	InInterface  string `json:"in_interface"`
	OutInterface string `json:"out_interface"`
	SourcePort   string `json:"source_port"`
	DestPort     string `json:"dest_port"`
	Target       string `json:"target"`
	Raw          string `json:"raw"`
}

func IPTablesRules(ctx context.Context) (IPTables, error) {
	v4, err := run(ctx, "iptables", "-S")
	if err != nil {
		return IPTables{}, err
	}

	v6, err := run(ctx, "ip6tables", "-S")
	if err != nil {
		return IPTables{IPv4: parseIPTables(v4)}, err
	}

	return IPTables{IPv4: parseIPTables(v4), IPv6: parseIPTables(v6)}, nil
}

func parseIPTables(out string) IPTablesFilter {
	filter := IPTablesFilter{Policies: map[string]string{}}

	for _, line := range lines(out) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		// '-P <chain> <target>' is the default policy of a chain, '-N <chain>' only declares it
		if fields[0] == "-P" {
			if len(fields) == 3 {
				filter.Policies[fields[1]] = fields[2]
			}
			continue
		}
		if fields[0] != "-A" {
			continue
		}

		rule := IPTablesRule{Chain: fields[1], Raw: line}
		for i := 2; i < len(fields)-1; i++ {
			switch fields[i] {
			case "-p":
				rule.Protocol = fields[i+1]
			case "-s":
				rule.Source = fields[i+1]
			case "-d":
				rule.Destination = fields[i+1]
			case "-i":
				rule.InInterface = fields[i+1]
			case "-o":
				rule.OutInterface = fields[i+1]
			case "--sport", "--sports":
				rule.SourcePort = fields[i+1]
			case "--dport", "--dports":
				rule.DestPort = fields[i+1]
			case "-j":
				rule.Target = fields[i+1]
			}
		}

		filter.Rules = append(filter.Rules, rule)
	}

	return filter
}
