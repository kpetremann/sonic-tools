package sonic

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
)

type RouteTable map[string][]RouteEntry

type RouteEntry struct {
	DestSelected             bool      `json:"destSelected"`
	Distance                 int       `json:"distance"`
	Installed                bool      `json:"installed"`
	InstalledNexthopGroupID  int       `json:"installedNexthopGroupId"`
	InternalFlags            int       `json:"internalFlags"`
	InternalNextHopActiveNum int       `json:"internalNextHopActiveNum"`
	InternalNextHopNum       int       `json:"internalNextHopNum"`
	InternalStatus           int       `json:"internalStatus"`
	Metric                   int       `json:"metric"`
	NexthopGroupID           int       `json:"nexthopGroupId"`
	Nexthops                 []Nexthop `json:"nexthops"`
	Prefix                   string    `json:"prefix"`
	PrefixLen                int       `json:"prefixLen"`
	Protocol                 string    `json:"protocol"`
	Selected                 bool      `json:"selected"`
	Table                    int       `json:"table"`
	Uptime                   string    `json:"uptime"`
	VrfID                    int       `json:"vrfId"`
	VrfName                  string    `json:"vrfName"`
}

type Nexthop struct {
	Active            bool   `json:"active"`
	Afi               string `json:"afi"`
	DirectlyConnected bool   `json:"directlyConnected"`
	Fib               bool   `json:"fib"`
	Flags             int    `json:"flags"`
	InterfaceIndex    int    `json:"interfaceIndex"`
	InterfaceName     string `json:"interfaceName"`
	IP                string `json:"ip"`
	Weight            int    `json:"weight"`
}

type Route struct {
	NextHopIP         string
	LocalInterface    string
	DirectlyConnected bool
}

// IPRoute returns the routes toward the given address.
func IPRoute(ctx context.Context, addr netip.Addr) (RouteTable, error) {
	family := "ip"
	if !addr.Is4() {
		family = "ipv6"
	}

	routeTable := RouteTable{}
	if err := vtyshJSON(ctx, &routeTable, fmt.Sprintf("show %s route %s json", family, addr)); err != nil {
		return nil, err
	}

	return routeTable, nil
}

// ExtractRoutes flattens a route table into its next-hops.
func ExtractRoutes(routeTable RouteTable) []Route {
	routes := []Route{}
	for _, entries := range routeTable {
		for _, entry := range entries {
			for _, nexthop := range entry.Nexthops {
				routes = append(routes, Route{nexthop.IP, nexthop.InterfaceName, nexthop.DirectlyConnected})
			}
		}
	}
	return routes
}

// OtherP2PHost returns the other host address of the provided CIDR.
//
// It assumes the input is a host in a p2p network (/30 or /31 for ipv4, /127 for ipv6).
func OtherP2PHost(prefix netip.Prefix) (netip.Prefix, error) {
	isV4 := prefix.Addr().Is4()

	switch mask := prefix.Bits(); {
	case isV4 && mask == 30:
		otherHost := prefix.Addr().As4()

		// check if network or broadcast address, we only need the last two bits (3 = 0b11)
		if last2bits := otherHost[3] & 3; last2bits == 0 || last2bits == 3 {
			return netip.Prefix{}, errors.New("not supported address: network or broadcast address")
		}

		otherHost[3] ^= 3 // toggle the last two bits to get the other host (using XOR 0b11)
		return netip.PrefixFrom(netip.AddrFrom4(otherHost), mask), nil

	case isV4 && mask == 31:
		otherHost := prefix.Addr().As4()
		otherHost[3] ^= 1 // toggle the last bit to get the other host (using XOR 0b01)
		return netip.PrefixFrom(netip.AddrFrom4(otherHost), mask), nil

	case !isV4 && mask == 127:
		otherHost := prefix.Addr().As16()
		otherHost[15] ^= 1 // toggle the last bit to get the other host (using XOR 0b01)
		return netip.PrefixFrom(netip.AddrFrom16(otherHost), mask), nil

	default:
		return netip.Prefix{}, errors.New("unsupported network: not point to point")
	}
}
