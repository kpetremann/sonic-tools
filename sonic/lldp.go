package sonic

import (
	"encoding/json"
	"fmt"
	"net"
)

type LLDP struct {
	LLDP LLDPInterfaces `json:"lldp"`
}

type LLDPInterfaces struct {
	Interface []map[string]InterfaceDetail `json:"interface"`
}

type InterfaceDetail struct {
	RID     string           `json:"rid"`
	Age     string           `json:"age"`
	Chassis ChassisContainer `json:"chassis"`
	Port    LLDPPort         `json:"port"`
}

type ChassisContainer map[string]Chassis

type Chassis struct {
	ID         ID     `json:"id"`
	Descr      string `json:"descr"`
	MgmtIP     any    `json:"mgmt-ip"`
	Capability any    `json:"capability"`
}

type ID struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type LLDPPort struct {
	ID    ID     `json:"id"`
	Descr string `json:"descr"`
	TTL   string `json:"ttl"`
}

// Neighbor holds the remote end of a link, as seen by LLDP.
type Neighbor struct {
	Host            string `json:"host"`
	Port            string `json:"port"`
	PortDescription string `json:"port_description"`
}

// PortName returns the most readable name of the remote port: its ID, or its description
// when the ID is only a MAC address, as reported by servers without an LLDP daemon.
func (n Neighbor) PortName() string {
	if isMACAddress(n.Port) && n.PortDescription != "" {
		return n.PortDescription
	}
	return n.Port
}

func LLDPNeighbors() (LLDP, error) {
	var lldp LLDP
	if err := runJSON(&lldp, "lldpctl", "-f", "json"); err != nil {
		return LLDP{}, err
	}
	return lldp, nil
}

// Neighbor returns the remote host and port seen on the given local interface.
func (l LLDP) Neighbor(intf string) Neighbor {
	neighbor := Neighbor{}

	for _, interfaces := range l.LLDP.Interface {
		info, exists := interfaces[intf]
		if !exists {
			continue
		}

		for hostname := range info.Chassis {
			neighbor.Host = hostname
		}

		neighbor.Port = info.Port.ID.Value
		neighbor.PortDescription = info.Port.Descr
	}

	return neighbor
}

func isMACAddress(mac string) bool {
	_, err := net.ParseMAC(mac)
	return err == nil
}

// UnmarshalJSON handles the two chassis layouts returned by lldpctl:
// keyed by hostname when the remote name is known, inlined otherwise.
func (cc *ChassisContainer) UnmarshalJSON(data []byte) error {
	*cc = make(map[string]Chassis)

	var chassis Chassis
	if err := json.Unmarshal(data, &chassis); err == nil {
		if chassis.ID.Type != "" || chassis.ID.Value != "" {
			key := chassis.ID.Value
			if key == "" {
				key = "N/A"
			}
			(*cc)[key] = chassis
			return nil
		}
	}

	var chassisMap map[string]Chassis
	if err := json.Unmarshal(data, &chassisMap); err != nil {
		return fmt.Errorf("failed to unmarshal chassis as either direct object or map: %w", err)
	}
	*cc = chassisMap

	return nil
}
