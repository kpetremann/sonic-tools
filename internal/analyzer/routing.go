package analyzer

import (
	"context"
	"log"
	"net/netip"
	"slices"
	"strings"

	"github.com/premday/sonic-tools/internal/view"
	"github.com/premday/sonic-tools/sonic"
)

type routeLocalAsset struct {
	Host           string         `json:"host"`
	Interface      string         `json:"interface"`
	InterfaceAlias string         `json:"interface_alias"`
	Addresses      []netip.Prefix `json:"addresses"`
}

type routeRemoteAsset struct {
	Host      string     `json:"host"`
	Interface string     `json:"interface"`
	Address   netip.Addr `json:"address"`
}

type Route struct {
	Local   routeLocalAsset  `json:"local"`
	Remote  routeRemoteAsset `json:"remote"`
	NextHop string           `json:"next_hop"`
}

type RoutingInfo struct {
	Routes []Route
}

func (r RoutingInfo) String() string {
	var buf strings.Builder
	buf.WriteString(view.Header("Routing"))

	if len(r.Routes) == 0 {
		buf.WriteString(view.Comment("No route found"))
		return buf.String()
	}

	t := view.NewTable("Interface", "Alias", "Address", "Next hop", "LLDP host", "LLDP port")
	for _, route := range r.Routes {
		localAddrs := []string{}
		for _, addr := range route.Local.Addresses {
			localAddrs = append(localAddrs, addr.String())
		}

		t.Row(
			route.Local.Interface,
			route.Local.InterfaceAlias,
			strings.Join(localAddrs, "|"),
			route.NextHop,
			route.Remote.Host,
			route.Remote.Interface,
		)
	}
	buf.WriteString(t.String())

	return buf.String()
}

func (a *IPAnalyzer) GetRoutingInfo(ctx context.Context) RoutingInfo {
	info := RoutingInfo{}

	for _, gw := range a.routes {
		// the addresses of every interface are already pre-fetched
		localAddresses := []string{}
		localPrefixes := []netip.Prefix{}
		for _, intf := range a.interfaceAddr {
			if intf.Name != gw.LocalInterface || intf.Prefix.Addr().Is4() != a.netIP.Is4() {
				continue
			}
			localAddresses = append(localAddresses, intf.Prefix.Addr().String())
			localPrefixes = append(localPrefixes, intf.Prefix)
		}

		remoteIP, _ := netip.ParseAddr(gw.NextHopIP)
		nextHop := gw.NextHopIP
		// if route is "connected" type, remote IP is the one passed to the CLI
		if gw.DirectlyConnected {
			nextHop = "connected"
			// unless the IP passed to the CLI is a local IP address
			if !slices.Contains(localAddresses, a.netIP.String()) {
				remoteIP = a.netIP
			} else if len(localPrefixes) > 0 {
				if otherIP, err := sonic.OtherP2PHost(localPrefixes[0]); err == nil {
					remoteIP = otherIP.Addr()
				}
			}
		}

		port, err := sonic.FindPortConfig(ctx, a.rdb, gw.LocalInterface)
		if err != nil {
			log.Println("failed to extract interface information:", err)
		}
		lldp := a.lldpNeighbors.Neighbor(gw.LocalInterface)

		info.Routes = append(info.Routes, Route{
			Local: routeLocalAsset{
				Host:           a.localHostname,
				Interface:      gw.LocalInterface,
				InterfaceAlias: port.Alias,
				Addresses:      localPrefixes,
			},
			Remote: routeRemoteAsset{
				Host:      lldp.Host,
				Interface: lldp.Port,
				Address:   remoteIP,
			},
			NextHop: nextHop,
		})
	}

	return info
}
