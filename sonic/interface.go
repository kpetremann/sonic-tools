package sonic

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	redis "github.com/redis/go-redis/v9"
)

// PortConfig is the CONFIG_DB PORT entry of an interface.
type PortConfig struct {
	AdminStatus string `redis:"admin_status" json:"admin_status"`
	Alias       string `redis:"alias" json:"alias"`
	Description string `redis:"description" json:"description"`
	FEC         string `redis:"fec" json:"fec"`
	Index       int    `redis:"index" json:"index"`
	Lanes       string `redis:"lanes" json:"lanes"`
	MTU         int    `redis:"mtu" json:"mtu"`
	Speed       int    `redis:"speed" json:"speed"`
}

// Interface is the aggregated state of a front panel port.
type Interface struct {
	Name string `json:"name"`
	PortConfig
	OperStatus string   `json:"oper_status"`
	Optic      Optic    `json:"optic"`
	Counters   Counters `json:"counters"`
	Neighbor   Neighbor `json:"neighbor"`
}

// Optic is the transceiver plugged in an interface. DOM values are null when the
// transceiver does not report them: temperature in °C, voltage in V, power in dBm, bias in mA.
type Optic struct {
	Present      bool            `json:"present"`
	Type         string          `json:"type"`
	Manufacturer string          `json:"manufacturer"`
	Model        string          `json:"model"`
	Serial       string          `json:"serial"`
	Temperature  NAFloat         `json:"temperature"`
	Voltage      NAFloat         `json:"voltage"`
	Lanes        []OpticLane     `json:"lanes"`
	Thresholds   OpticThresholds `json:"thresholds"`
}

type OpticLane struct {
	Index   int     `json:"index"`
	RXPower NAFloat `json:"rx_power"`
	TXPower NAFloat `json:"tx_power"`
	TXBias  NAFloat `json:"tx_bias"`
}

// OpticThresholds are the alarm levels of the transceiver, a value outside of them breaks the link.
type OpticThresholds struct {
	TemperatureHigh NAFloat `json:"temperature_high"`
	VoltageLow      NAFloat `json:"voltage_low"`
	VoltageHigh     NAFloat `json:"voltage_high"`
	RXPowerLow      NAFloat `json:"rx_power_low"`
	RXPowerHigh     NAFloat `json:"rx_power_high"`
	TXPowerLow      NAFloat `json:"tx_power_low"`
	TXPowerHigh     NAFloat `json:"tx_power_high"`
}

// Counters are the SAI counters of a port. A counter is null when the ASIC does not publish it,
// so that 0 always means "nothing counted": the FEC ones are only reported by some platforms,
// and none of them reports CRC align errors.
type Counters struct {
	InOctets         *uint64 `redis:"SAI_PORT_STAT_IF_IN_OCTETS" json:"in_octets"`
	OutOctets        *uint64 `redis:"SAI_PORT_STAT_IF_OUT_OCTETS" json:"out_octets"`
	InErrors         *uint64 `redis:"SAI_PORT_STAT_IF_IN_ERRORS" json:"in_errors"`
	OutErrors        *uint64 `redis:"SAI_PORT_STAT_IF_OUT_ERRORS" json:"out_errors"`
	InDiscards       *uint64 `redis:"SAI_PORT_STAT_IF_IN_DISCARDS" json:"in_discards"`
	OutDiscards      *uint64 `redis:"SAI_PORT_STAT_IF_OUT_DISCARDS" json:"out_discards"`
	InDrops          *uint64 `redis:"SAI_PORT_STAT_IN_DROPPED_PKTS" json:"in_drops"`
	OutDrops         *uint64 `redis:"SAI_PORT_STAT_OUT_DROPPED_PKTS" json:"out_drops"`
	FECCorrectable   *uint64 `redis:"SAI_PORT_STAT_IF_IN_FEC_CORRECTABLE_FRAMES" json:"fec_correctable"`
	FECUncorrectable *uint64 `redis:"SAI_PORT_STAT_IF_IN_FEC_NOT_CORRECTABLE_FRAMES" json:"fec_uncorrectable"`
	FECSymbolErrors  *uint64 `redis:"SAI_PORT_STAT_IF_IN_FEC_SYMBOL_ERRORS" json:"fec_symbol_errors"`
}

// interfaceDBs are the four databases the state of an interface is spread over. They are held
// together so that enriching one port and enriching every port share the same connections.
type interfaceDBs struct {
	config, appl, state, counters *redis.Conn
}

func openInterfaceDBs(ctx context.Context, rdb *redis.Client) (*interfaceDBs, error) {
	dbs := &interfaceDBs{}

	for _, open := range []struct {
		conn **redis.Conn
		db   int
	}{
		{&dbs.config, CONFIGDB}, {&dbs.appl, APPLDB},
		{&dbs.state, STATEDB}, {&dbs.counters, COUNTERSDB},
	} {
		conn, err := openDB(ctx, rdb, open.db)
		if err != nil {
			dbs.Close()
			return nil, err
		}
		*open.conn = conn
	}

	return dbs, nil
}

func (d *interfaceDBs) Close() {
	for _, conn := range []*redis.Conn{d.config, d.appl, d.state, d.counters} {
		if conn != nil {
			conn.Close()
		}
	}
}

// portState returns the state of one port. oids comes from interfaceOIDs, it is read once for
// a whole collection rather than per port.
func (d *interfaceDBs) portState(ctx context.Context, lldp LLDP, name string, oids map[string]string) (Interface, error) {
	intf := Interface{Name: name, Neighbor: lldp.Neighbor(name)}
	if err := d.config.HGetAll(ctx, "PORT|"+name).Scan(&intf.PortConfig); err != nil {
		return Interface{}, fmt.Errorf("failed to get configuration of %s: %w", name, err)
	}

	var err error
	if intf.OperStatus, err = operStatus(ctx, d.appl, name); err != nil {
		return Interface{}, err
	}

	if intf.Optic, err = portOptic(ctx, d.state, name, len(strings.Split(intf.Lanes, ","))); err != nil {
		return Interface{}, err
	}

	if oid, exists := oids[name]; exists {
		if err := d.counters.HGetAll(ctx, "COUNTERS:"+oid).Scan(&intf.Counters); err != nil {
			return Interface{}, fmt.Errorf("failed to get counters of %s: %w", name, err)
		}
	}

	return intf, nil
}

// Interfaces returns every front panel port with its status, transceiver and counters,
// and the LLDP neighbor seen on it.
func Interfaces(ctx context.Context, rdb *redis.Client, lldp LLDP) ([]Interface, error) {
	dbs, err := openInterfaceDBs(ctx, rdb)
	if err != nil {
		return nil, err
	}
	defer dbs.Close()

	keys, err := scanKeys(ctx, dbs.config, "PORT|*")
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(keys))
	for _, key := range keys {
		names = append(names, strings.TrimPrefix(key, "PORT|"))
	}
	sortNames(names)

	oids, err := interfaceOIDs(ctx, dbs.counters)
	if err != nil {
		return nil, err
	}

	interfaces := make([]Interface, 0, len(names))
	for _, name := range names {
		intf, err := dbs.portState(ctx, lldp, name, oids)
		if err != nil {
			return nil, err
		}
		interfaces = append(interfaces, intf)
	}

	return interfaces, nil
}

// FindInterface returns the aggregated state of a single interface. It reads that port only,
// so it does not pay for the ports it is not asked about.
func FindInterface(ctx context.Context, rdb *redis.Client, lldp LLDP, name string) (Interface, error) {
	dbs, err := openInterfaceDBs(ctx, rdb)
	if err != nil {
		return Interface{}, err
	}
	defer dbs.Close()

	// an absent port would otherwise read as an interface with every field empty
	exists, err := dbs.config.Exists(ctx, "PORT|"+name).Result()
	if err != nil {
		return Interface{}, fmt.Errorf("failed to check interface %s: %w", name, err)
	}
	if exists == 0 {
		return Interface{}, fmt.Errorf("interface %s not found", name)
	}

	oids, err := interfaceOIDs(ctx, dbs.counters)
	if err != nil {
		return Interface{}, err
	}

	return dbs.portState(ctx, lldp, name, oids)
}

func portOptic(ctx context.Context, state *redis.Conn, name string, lanes int) (Optic, error) {
	info, err := state.HGetAll(ctx, "TRANSCEIVER_INFO|"+name).Result()
	if err != nil {
		return Optic{}, fmt.Errorf("failed to get transceiver of %s: %w", name, err)
	}
	if len(info) == 0 {
		return Optic{}, nil
	}

	sensors, err := state.HGetAll(ctx, "TRANSCEIVER_DOM_SENSOR|"+name).Result()
	if err != nil {
		return Optic{}, fmt.Errorf("failed to get transceiver sensors of %s: %w", name, err)
	}

	thresholds, err := state.HGetAll(ctx, "TRANSCEIVER_DOM_THRESHOLD|"+name).Result()
	if err != nil {
		return Optic{}, fmt.Errorf("failed to get transceiver thresholds of %s: %w", name, err)
	}
	// releases before 202111 keep the thresholds in the sensor table
	if len(thresholds) == 0 {
		thresholds = sensors
	}

	optic := Optic{
		Present:      true,
		Type:         pick(info, "type"),
		Manufacturer: pick(info, "manufacturer", "manufacturename", "vendor_name"),
		Model:        pick(info, "model", "modelname"),
		Serial:       pick(info, "serial", "serialnum"),
		Temperature:  domValue(sensors, "temperature"),
		Voltage:      domValue(sensors, "voltage"),
		Thresholds: OpticThresholds{
			TemperatureHigh: domValue(thresholds, "temphighalarm"),
			VoltageLow:      domValue(thresholds, "vcclowalarm"),
			VoltageHigh:     domValue(thresholds, "vcchighalarm"),
			RXPowerLow:      domValue(thresholds, "rxpowerlowalarm"),
			RXPowerHigh:     domValue(thresholds, "rxpowerhighalarm"),
			TXPowerLow:      domValue(thresholds, "txpowerlowalarm"),
			TXPowerHigh:     domValue(thresholds, "txpowerhighalarm"),
		},
	}

	for lane := 1; lane <= lanes; lane++ {
		values := OpticLane{
			Index:   lane,
			RXPower: domValue(sensors, fmt.Sprintf("rx%dpower", lane)),
			TXPower: domValue(sensors, fmt.Sprintf("tx%dpower", lane)),
			TXBias:  domValue(sensors, fmt.Sprintf("tx%dbias", lane)),
		}
		// direct attach and passive cables report nothing per lane
		if values.RXPower.Value == nil && values.TXPower.Value == nil && values.TXBias.Value == nil {
			continue
		}
		optic.Lanes = append(optic.Lanes, values)
	}

	return optic, nil
}

// operStatus returns the operational status of a port, of a port-channel or of the management
// interface. VLAN interfaces are not covered: APPL_DB does not report their status.
func operStatus(ctx context.Context, appl *redis.Conn, name string) (string, error) {
	for _, table := range []string{"PORT_TABLE:", "LAG_TABLE:", "MGMT_PORT_TABLE:"} {
		status, err := appl.HGet(ctx, table+name, "oper_status").Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to get status of %s: %w", name, err)
		}
		return status, nil
	}

	return "", nil
}

// domValue returns a DOM measure, it is null when the transceiver reports 'N/A'.
func domValue(fields map[string]string, name string) NAFloat {
	measure := NAFloat{}
	_ = measure.UnmarshalText([]byte(fields[name]))

	return measure
}

// FindPortConfig returns the CONFIG_DB PORT entry of an interface.
func FindPortConfig(ctx context.Context, rdb *redis.Client, intf string) (PortConfig, error) {
	conn, err := openDB(ctx, rdb, CONFIGDB)
	if err != nil {
		return PortConfig{}, err
	}
	defer conn.Close()

	var config PortConfig
	if err := conn.HGetAll(ctx, "PORT|"+intf).Scan(&config); err != nil {
		return PortConfig{}, fmt.Errorf("failed to get configuration of %s: %w", intf, err)
	}

	return config, nil
}

// SetPortDescription writes the description of an interface in CONFIG_DB.
func SetPortDescription(ctx context.Context, rdb *redis.Client, intf, description string) error {
	conn, err := openDB(ctx, rdb, CONFIGDB)
	if err != nil {
		return err
	}
	defer conn.Close()

	key := "PORT|" + intf
	exists, err := conn.Exists(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to check interface %s: %w", intf, err)
	}
	if exists == 0 {
		return fmt.Errorf("interface %s does not exist", intf)
	}

	if err := conn.HSet(ctx, key, "description", description).Err(); err != nil {
		return fmt.Errorf("failed to set description of %s: %w", intf, err)
	}

	return nil
}

// InterfaceNeighbors returns the expected neighbor of each interface, from the CONFIG_DB DEVICE_NEIGHBOR table.
func InterfaceNeighbors(ctx context.Context, rdb *redis.Client) (map[string]string, error) {
	conn, err := openDB(ctx, rdb, CONFIGDB)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	keys, err := scanKeys(ctx, conn, "DEVICE_NEIGHBOR|*")
	if err != nil {
		return nil, err
	}

	neighbors := make(map[string]string, len(keys))
	for _, key := range keys {
		intf := strings.TrimPrefix(key, "DEVICE_NEIGHBOR|")
		neighbors[intf] = conn.HGet(ctx, key, "name").Val()
	}

	return neighbors, nil
}

type InterfaceAddr struct {
	Name   string       `json:"name"`
	Prefix netip.Prefix `json:"prefix"`
}

// InterfacesAddrs returns the IP addresses of all interfaces, from the APPL_DB INTF_TABLE.
func InterfacesAddrs(ctx context.Context, rdb *redis.Client) ([]InterfaceAddr, error) {
	conn, err := openDB(ctx, rdb, APPLDB)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	keys, err := scanKeys(ctx, conn, "INTF_TABLE:*:*")
	if err != nil {
		return nil, err
	}

	addrs := make([]InterfaceAddr, 0, len(keys))
	for _, key := range keys {
		name, prefix, found := strings.Cut(strings.TrimPrefix(key, "INTF_TABLE:"), ":")
		if !found {
			continue
		}
		addr, err := netip.ParsePrefix(prefix)
		if err != nil {
			return nil, fmt.Errorf("invalid prefix in '%s': %w", key, err)
		}

		addrs = append(addrs, InterfaceAddr{name, addr})
	}

	return addrs, nil
}

// IPInterface returns the IP addresses of a single interface.
func IPInterface(ctx context.Context, rdb *redis.Client, intf string) ([]netip.Prefix, error) {
	addrs, err := InterfacesAddrs(ctx, rdb)
	if err != nil {
		return nil, err
	}

	prefixes := []netip.Prefix{}
	for _, addr := range addrs {
		if addr.Name == intf {
			prefixes = append(prefixes, addr.Prefix)
		}
	}

	return prefixes, nil
}

// interfaceOIDs returns the SAI object ID of each interface, they are the keys of the counter tables.
func interfaceOIDs(ctx context.Context, counters *redis.Conn) (map[string]string, error) {
	oids := map[string]string{}

	for _, table := range []string{"COUNTERS_PORT_NAME_MAP", "COUNTERS_LAG_NAME_MAP"} {
		names, err := counters.HGetAll(ctx, table).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get %s: %w", table, err)
		}

		// the LAG map holds an empty entry when no port-channel is configured
		for name, oid := range names {
			if name == "" || oid == "" {
				continue
			}
			oids[name] = oid
		}
	}

	return oids, nil
}
