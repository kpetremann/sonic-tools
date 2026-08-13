package analyzer

import (
	"context"
	"fmt"
	"log"
	"net/netip"
	"os"
	"strings"

	"github.com/premday/sonic-tools/internal/view"
	"github.com/premday/sonic-tools/sonic"

	redis "github.com/redis/go-redis/v9"
)

// IPAnalyzer find the details about an IP address: is it attached to an interface, is it routed? what is/are the neighbor.s?
//
// There are four sections:
//   - ARP/NDP/FDB: neighbor resolution from kernel + ASIC
//   - interface IP address: matching interfaces from APPL_DB INTF_TABLE
//   - interface description: remote peer parsed from CONFIG_DB PORT description
//   - routing: FRR route table
//
// The remote neighbor is found using the LLDP info. If LLDP is not available, it tries to parse the description of the interface.
//
// The neighbor IP address is inferred from the local IP when attached to a point to point network (/30, /31, /127).
//
// It supports IPv6.
// It does not support VRF.
type IPAnalyzer struct {
	netIP netip.Addr

	rdb *redis.Client

	localHostname    string
	routes           []sonic.Route
	interfaceAddr    []sonic.InterfaceAddr
	lldpNeighbors    sonic.LLDP
	resolvedNeighbor sonic.ResolvedNeighbor
}

// NewIPAnalyzer create an analyze and immediately starts pre-fetching data from the device.
func NewIPAnalyzer(ctx context.Context, rdb *redis.Client, targetIP string) (IPAnalyzer, error) {
	a := IPAnalyzer{rdb: rdb}
	var err error

	a.netIP, err = netip.ParseAddr(targetIP)
	if err != nil {
		return IPAnalyzer{}, fmt.Errorf("invalid IP address: %w", err)
	}

	a.localHostname, err = os.Hostname()
	if err != nil {
		a.localHostname = "local"
	}

	routeTable, err := sonic.IPRoute(ctx, a.netIP)
	if err != nil {
		return IPAnalyzer{}, fmt.Errorf("failed to get route table for %s: %w", targetIP, err)
	}
	a.routes = sonic.ExtractRoutes(routeTable)

	a.interfaceAddr, err = sonic.InterfacesAddrs(ctx, rdb)
	if err != nil {
		return IPAnalyzer{}, err
	}

	a.resolvedNeighbor, err = sonic.ResolveNeighborInterface(ctx, rdb, a.netIP)
	if err != nil {
		log.Println("failed to resolve neighbor interface:", err)
	}

	a.lldpNeighbors, err = sonic.LLDPNeighbors(ctx)
	if err != nil {
		log.Println("failed to get LLDP data:", err)
	}

	return a, nil
}

// NeighborInfo holds the formatted neighbor resolution result for an IP address.
type NeighborInfo struct {
	Neighbor sonic.ResolvedNeighbor `json:"neighbor"`
	Alias    string                 `json:"alias"`
	LLDP     sonic.Neighbor         `json:"lldp"`
}

func (n NeighborInfo) String() string {
	var buf strings.Builder
	buf.WriteString(view.Header("ARP/NDP/FDB"))

	if !n.Neighbor.Found {
		buf.WriteString(view.Comment("Not found"))
		return buf.String()
	}

	resolvedFrom := "direct"
	if n.Neighbor.IsVlan {
		resolvedFrom = fmt.Sprintf("Vlan%d (FDB)", n.Neighbor.VlanID)
	}
	if n.Neighbor.Iface == "" {
		resolvedFrom = "MAC not found in FDB"
	}

	t := view.NewTable("MAC", "Interface", "Alias", "Resolved from", "LLDP host", "LLDP port")
	t.Row(n.Neighbor.MAC, n.Neighbor.Iface, n.Alias, resolvedFrom, n.LLDP.Host, n.LLDP.Port)
	buf.WriteString(t.String())

	return buf.String()
}

// GetNeighborInfo returns the resolved neighbor information for the target IP,
// including the real physical/logical interface when the neighbor is on a VLAN.
func (a *IPAnalyzer) GetNeighborInfo(ctx context.Context) NeighborInfo {
	info := NeighborInfo{Neighbor: a.resolvedNeighbor}

	if a.resolvedNeighbor.Found && a.resolvedNeighbor.Iface != "" {
		info.LLDP = a.lldpNeighbors.Neighbor(a.resolvedNeighbor.Iface)

		port, err := sonic.FindPortConfig(ctx, a.rdb, a.resolvedNeighbor.Iface)
		if err != nil {
			log.Println("failed to extract interface information:", err)
		}
		info.Alias = port.Alias
	}

	return info
}
