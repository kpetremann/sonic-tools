package analyzer

import (
	"context"
	"log"
	"net/netip"
	"strings"

	"github.com/premday/sonic-tools/internal/view"
	"github.com/premday/sonic-tools/sonic"
)

// InterfaceLink holds all per-interface data for the merged Interface section.
type InterfaceLink struct {
	Interface   string         `json:"interface"`
	Alias       string         `json:"alias"`
	Description string         `json:"description"`
	LLDP        sonic.Neighbor `json:"lldp"`
	Address     netip.Prefix   `json:"address"`
}

// InterfaceInfo holds interfaces whose IP subnet contains the target IP,
// enriched with CONFIG_DB description and LLDP neighbour data.
type InterfaceInfo struct {
	Links []InterfaceLink
}

func (i InterfaceInfo) String() string {
	var buf strings.Builder
	buf.WriteString(view.Header("Matching interface"))

	if len(i.Links) == 0 {
		buf.WriteString(view.Comment("No matching interface"))
		return buf.String()
	}

	t := view.NewTable("Interface", "Alias", "Address", "Description", "LLDP host", "LLDP port")
	for _, link := range i.Links {
		t.Row(link.Interface, link.Alias, link.Address.String(), link.Description, link.LLDP.Host, link.LLDP.Port)
	}
	buf.WriteString(t.String())

	return buf.String()
}

// GetInterfaceInfo returns interface information for all interfaces whose IP
// subnet contains the target IP, merging address, description and LLDP data.
func (a *IPAnalyzer) GetInterfaceInfo(ctx context.Context) InterfaceInfo {
	info := InterfaceInfo{}

	for _, intf := range a.interfaceAddr {
		if !intf.Prefix.Contains(a.netIP) {
			continue
		}

		port, err := sonic.FindPortConfig(ctx, a.rdb, intf.Name)
		if err != nil {
			log.Println("failed to extract interface information:", err)
		}

		info.Links = append(info.Links, InterfaceLink{
			Interface:   intf.Name,
			Alias:       port.Alias,
			Description: port.Description,
			LLDP:        a.lldpNeighbors.Neighbor(intf.Name),
			Address:     intf.Prefix,
		})
	}

	return info
}
