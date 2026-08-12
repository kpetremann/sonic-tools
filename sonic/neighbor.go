package sonic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	redis "github.com/redis/go-redis/v9"
	"github.com/vishvananda/netlink"
)

type IPNeighbor struct {
	Interface string `json:"interface"`
	Alias     string `json:"alias"`
	MAC       string `json:"mac"`
	Found     bool   `json:"found"`
}

// NetlinkNeighbor returns the kernel ARP/NDP entry of an address.
func NetlinkNeighbor(addr netip.Addr) (IPNeighbor, error) {
	family := netlink.FAMILY_V6
	if addr.Is4() {
		family = netlink.FAMILY_V4
	}

	neighbors, err := netlink.NeighList(0, family)
	if err != nil {
		return IPNeighbor{}, fmt.Errorf("failed to list neighbors: %w", err)
	}

	for _, neighbor := range neighbors {
		if ip, err := netip.ParseAddr(neighbor.IP.String()); err != nil || ip != addr {
			continue
		}

		link, err := netlink.LinkByIndex(neighbor.LinkIndex)
		if err != nil {
			return IPNeighbor{}, fmt.Errorf("unable to get interface from index '%d': %w", neighbor.LinkIndex, err)
		}

		return IPNeighbor{
			Interface: link.Attrs().Name,
			Alias:     link.Attrs().Alias,
			MAC:       neighbor.HardwareAddr.String(),
			Found:     true,
		}, nil
	}

	return IPNeighbor{}, nil
}

type ResolvedNeighbor struct {
	IP     netip.Addr `json:"ip"`
	MAC    string     `json:"mac"`
	Iface  string     `json:"iface"`
	VlanID int        `json:"vlan_id"`
	IsVlan bool       `json:"is_vlan"`
	Found  bool       `json:"found"`
}

var vlanRegex = regexp.MustCompile(`^Vlan(\d+)$`)

// ResolveNeighborInterface finds the real physical/logical interface for a
// given IP address. On SONiC, when a neighbor is learned on a VLAN interface
// (e.g. Vlan1000), the kernel neighbor table reports the VLAN interface name
// and the MAC address. This function looks up that MAC in the ASIC_DB FDB
// table to find the actual port (e.g. Ethernet20, PortChannel0001) where the
// MAC was learned.
//
// The resolution follows the same logic as the SONiC nbrshow utility.
func ResolveNeighborInterface(ctx context.Context, rdb *redis.Client, addr netip.Addr) (ResolvedNeighbor, error) {
	neighbor, err := NetlinkNeighbor(addr)
	if err != nil {
		return ResolvedNeighbor{}, fmt.Errorf("failed to fetch neighbor for %s: %w", addr, err)
	}
	if !neighbor.Found {
		return ResolvedNeighbor{}, nil
	}

	resolved := ResolvedNeighbor{
		IP:    addr,
		MAC:   neighbor.MAC,
		Iface: neighbor.Interface,
		Found: true,
	}

	matches := vlanRegex.FindStringSubmatch(neighbor.Interface)
	if matches == nil {
		return resolved, nil
	}
	vlanID, err := strconv.Atoi(matches[1])
	if err != nil {
		return resolved, nil //nolint:nilerr // we return as-is if invalid VLAN ID
	}
	resolved.IsVlan = true
	resolved.VlanID = vlanID

	entries, err := MACTable(ctx, rdb)
	if err != nil {
		return resolved, err
	}

	// the interface is left empty when the MAC is not in the FDB anymore
	resolved.Iface = ""
	for _, entry := range entries {
		if entry.VlanID == vlanID && strings.EqualFold(entry.MAC, neighbor.MAC) {
			resolved.Iface = entry.Iface
			break
		}
	}

	return resolved, nil
}

type FDBEntry struct {
	VlanID int    `json:"vlan_id"`
	MAC    string `json:"mac"`
	Iface  string `json:"iface"`
}

// MACTable returns the FDB entries of the ASIC, with their interface resolved.
//
// It requires the following lookups in Redis:
//   - COUNTERS_DB: COUNTERS_PORT_NAME_MAP + COUNTERS_LAG_NAME_MAP -> OID-to-interface-name map
//   - ASIC_DB: SAI_OBJECT_TYPE_BRIDGE_PORT -> bridge-port-OID to port-OID map
//   - ASIC_DB: SAI_OBJECT_TYPE_FDB_ENTRY -> (VLAN, MAC) to bridge-port-OID
//   - ASIC_DB: SAI_OBJECT_TYPE_VLAN -> bvid to VLAN ID (when needed)
func MACTable(ctx context.Context, rdb *redis.Client) ([]FDBEntry, error) {
	counters, err := openDB(ctx, rdb, COUNTERSDB)
	if err != nil {
		return nil, err
	}
	defer counters.Close()

	oids, err := interfaceOIDs(ctx, counters)
	if err != nil {
		return nil, err
	}
	interfaceNames := make(map[string]string, len(oids))
	for name, oid := range oids {
		interfaceNames[strings.TrimPrefix(oid, oidPrefix)] = name
	}

	asic, err := openDB(ctx, rdb, ASICDB)
	if err != nil {
		return nil, err
	}
	defer asic.Close()

	bridgePorts, err := bridgePortMap(ctx, asic)
	if err != nil {
		return nil, err
	}

	keys, err := scanKeys(ctx, asic, "ASIC_STATE:SAI_OBJECT_TYPE_FDB_ENTRY:*")
	if err != nil {
		return nil, err
	}

	entries := make([]FDBEntry, 0, len(keys))
	for _, key := range keys {
		// the key format is: ASIC_STATE:SAI_OBJECT_TYPE_FDB_ENTRY:{json}
		parts := strings.SplitN(key, ":", 3)
		if len(parts) < 3 {
			continue
		}

		fdbKey := struct {
			BvID string `json:"bvid"`
			MAC  string `json:"mac"`
			Vlan string `json:"vlan"`
		}{}
		if err := json.Unmarshal([]byte(parts[2]), &fdbKey); err != nil {
			continue
		}

		bridgePort, err := asic.HGet(ctx, key, "SAI_FDB_ENTRY_ATTR_BRIDGE_PORT_ID").Result()
		if err != nil {
			continue
		}
		port, exists := bridgePorts[strings.TrimPrefix(bridgePort, oidPrefix)]
		if !exists {
			continue
		}

		name, exists := interfaceNames[port]
		if !exists {
			name = port
		}

		vlanID, err := fdbVlanID(ctx, asic, fdbKey.Vlan, fdbKey.BvID)
		if err != nil {
			continue
		}

		entries = append(entries, FDBEntry{VlanID: vlanID, MAC: strings.ToUpper(fdbKey.MAC), Iface: name})
	}

	return entries, nil
}

const oidPrefix = "oid:0x"

// bridgePortMap returns the port OID of each bridge port OID.
func bridgePortMap(ctx context.Context, asic *redis.Conn) (map[string]string, error) {
	keys, err := scanKeys(ctx, asic, "ASIC_STATE:SAI_OBJECT_TYPE_BRIDGE_PORT:*")
	if err != nil {
		return nil, err
	}

	bridgePorts := make(map[string]string, len(keys))
	for _, key := range keys {
		port, err := asic.HGet(ctx, key, "SAI_BRIDGE_PORT_ATTR_PORT_ID").Result()
		if err != nil {
			continue
		}

		bridgePortID := strings.TrimPrefix(key, "ASIC_STATE:SAI_OBJECT_TYPE_BRIDGE_PORT:"+oidPrefix)
		bridgePorts[bridgePortID] = strings.TrimPrefix(port, oidPrefix)
	}

	return bridgePorts, nil
}

// fdbVlanID returns the VLAN ID of an FDB entry, resolving the bridge VLAN OID when the VLAN is not in the key.
func fdbVlanID(ctx context.Context, asic *redis.Conn, vlan, bvid string) (int, error) {
	if vlan != "" {
		return strconv.Atoi(vlan)
	}

	key := "ASIC_STATE:SAI_OBJECT_TYPE_VLAN:" + bvid
	vlanID, err := asic.HGet(ctx, key, "SAI_VLAN_ATTR_VLAN_ID").Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get VLAN ID for bvid %s: %w", bvid, err)
	}

	return strconv.Atoi(vlanID)
}
