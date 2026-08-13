# SONiC tools

## SONiC library

This repository exposes a SONiC Go library (mainly for read purposes).

Documentation can be found here: [https://pkg.go.dev/github.com/premday/sonic-tools](https://pkg.go.dev/github.com/premday/sonic-tools)

## Tools

Two CLI tools to operate Community [SONiC](https://sonic-net.github.io/SONiC/) switches.
Both tools connect to the local Redis instance on `127.0.0.1:6379` and must be run directly on the switch.

Everything they read from the switch lives in the `sonic` package, which can be used as a library.

### premshow

Collects the state of the switch, either section per section or as a whole.
The `--json` flag outputs the result as JSON instead of a formatted table, which is how automated
device validation consumes it.

```
# whole device state, every section in one document
premshow all [--json]

# one section at a time
premshow platform            # platform, HwSKU, MAC, serial number, image version
premshow interfaces [intf]   # status, speed, description, transceiver, counters, LLDP neighbor
premshow optical [intf]      # transceiver DOM values and their alarm thresholds
premshow bgp [neighbor]      # neighbors state, uptime, prefix counters, expected interface
premshow route-map [name]    # BGP route-maps, with their rules for a given one
premshow psu                 # power supplies
premshow fan                 # fans
premshow temperature         # temperature sensors
premshow containers          # docker containers
premshow dhcp-relay          # number of relay processes in the dhcp_relay container
premshow users               # local users
premshow snmp                # SNMP configuration
premshow iptables            # iptables and ip6tables rules
premshow sysctl              # some kernel settings
```

`premshow all` never fails on a single section: what could not be collected is reported in `Errors`.

It also aggregates information about a given IP address into a single command, replacing several
native SONiC commands:

```
premshow ip <address> [--json]
```

For a given IP address it displays:

- **Neighbor** — ARP/NDP entry (MAC address, interface, VLAN)
- **Interface** — interfaces whose subnet contains the IP, with address, description, and LLDP neighbor
- **Routing** — matching routes with next-hops

### premconfig

Manages switch configuration.

```
# Set an interface description manually
premconfig interface description <intf> <description>

# Set interface descriptions automatically from LLDP neighbor data
premconfig interface auto-description <intf|all> [prefix]
```

The `auto-description` command reads LLDP neighbor data and writes the remote hostname (and port) as the interface description.
Passing `all` instead of an interface name applies the update to every interface.
An optional prefix is prepended to every generated description.

Both subcommands accept a `--dry-run` flag to preview changes without applying them.
`auto-description` also accepts `-v` / `--verbose` to include skipped interfaces in the output.
