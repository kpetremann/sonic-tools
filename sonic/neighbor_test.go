package sonic

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

// asicRedis builds the two databases MACTable reads. No dump of ASIC_DB was collected from a
// device, so the entries below are written by hand, in the format the SAI layer uses:
//   - the FDB key carries the VLAN on some platforms, only the bridge VLAN OID on the others
//   - an entry can point at a bridge port, a port or a VLAN which is not published
func asicRedis(t *testing.T) *redis.Client {
	t.Helper()

	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { rdb.Close() })

	server.Select(COUNTERSDB)
	server.HSet("COUNTERS_PORT_NAME_MAP", "Ethernet0", "oid:0x1000000000001")
	server.HSet("COUNTERS_PORT_NAME_MAP", "Ethernet8", "oid:0x1000000000002")
	server.HSet("COUNTERS_LAG_NAME_MAP", "", "") // as both sampled devices publish it

	server.Select(ASICDB)
	bridgePorts := map[string]string{
		"oid:0x3a00000001": "oid:0x1000000000001", // Ethernet0
		"oid:0x3a00000002": "oid:0x1000000000002", // Ethernet8
		"oid:0x3a00000003": "oid:0x1000000000009", // a port absent from the name maps
	}
	for bridgePort, port := range bridgePorts {
		server.HSet("ASIC_STATE:SAI_OBJECT_TYPE_BRIDGE_PORT:"+bridgePort, "SAI_BRIDGE_PORT_ATTR_PORT_ID", port)
	}
	// a bridge port which does not publish its port, it resolves to nothing
	server.HSet("ASIC_STATE:SAI_OBJECT_TYPE_BRIDGE_PORT:oid:0x3a00000004", "SAI_BRIDGE_PORT_ATTR_TYPE", "SAI_BRIDGE_PORT_TYPE_PORT")

	server.HSet("ASIC_STATE:SAI_OBJECT_TYPE_VLAN:oid:0x2600000123", "SAI_VLAN_ATTR_VLAN_ID", "506")

	entries := []struct {
		key        string
		bridgePort string
	}{
		// two MACs behind the same bridge VLAN OID: it must be resolved once and used twice
		{`{"bvid":"oid:0x2600000123","mac":"00:00:5E:00:53:01","switch_id":"oid:0x21"}`, "oid:0x3a00000001"},
		{`{"bvid":"oid:0x2600000123","mac":"00:00:5e:00:53:02","switch_id":"oid:0x21"}`, "oid:0x3a00000002"},
		// the VLAN is in the key, no lookup needed
		{`{"bvid":"oid:0x2600000123","mac":"00:00:5E:00:53:03","vlan":"777","switch_id":"oid:0x21"}`, "oid:0x3a00000001"},
		// the port behind this bridge port is not in the name maps, the OID is reported instead
		{`{"bvid":"oid:0x2600000123","mac":"00:00:5E:00:53:04","switch_id":"oid:0x21"}`, "oid:0x3a00000003"},
		// dropped: the bridge VLAN OID resolves to nothing
		{`{"bvid":"oid:0x2600000999","mac":"00:00:5E:00:53:05","switch_id":"oid:0x21"}`, "oid:0x3a00000001"},
		// dropped: unknown bridge port
		{`{"bvid":"oid:0x2600000123","mac":"00:00:5E:00:53:06","switch_id":"oid:0x21"}`, "oid:0x3a00000099"},
		// dropped: the bridge port publishes no port
		{`{"bvid":"oid:0x2600000123","mac":"00:00:5E:00:53:07","switch_id":"oid:0x21"}`, "oid:0x3a00000004"},
	}
	for _, entry := range entries {
		key := "ASIC_STATE:SAI_OBJECT_TYPE_FDB_ENTRY:" + entry.key
		server.HSet(key, "SAI_FDB_ENTRY_ATTR_BRIDGE_PORT_ID", entry.bridgePort)
		server.HSet(key, "SAI_FDB_ENTRY_ATTR_TYPE", "SAI_FDB_ENTRY_TYPE_DYNAMIC")
	}

	// dropped: the key is not the JSON the ASIC writes
	server.HSet("ASIC_STATE:SAI_OBJECT_TYPE_FDB_ENTRY:not-json", "SAI_FDB_ENTRY_ATTR_BRIDGE_PORT_ID", "oid:0x3a00000001")

	return rdb
}

func TestMACTable(t *testing.T) {
	entries, err := MACTable(context.Background(), asicRedis(t))
	if err != nil {
		t.Fatal(err)
	}

	// the MAC is published in upper case, whatever the ASIC wrote
	want := map[string]string{
		"00:00:5E:00:53:01": "506/Ethernet0",
		"00:00:5E:00:53:02": "506/Ethernet8",
		"00:00:5E:00:53:03": "777/Ethernet0",
		"00:00:5E:00:53:04": "506/1000000000009",
	}

	got := map[string]string{}
	for _, entry := range entries {
		got[entry.MAC] = fmt.Sprintf("%d/%s", entry.VlanID, entry.Iface)
	}

	if len(got) != len(want) {
		t.Errorf("want %d entries, got %d: %v", len(want), len(got), got)
	}
	for mac, location := range want {
		if got[mac] != location {
			t.Errorf("%s: want %q, got %q", mac, location, got[mac])
		}
	}
}

// TestFDBVlanIDsReadsEachVlanOnce pins the lookup which used to be done per entry: a busy ToR
// holds thousands of MACs behind a handful of VLANs.
func TestFDBVlanIDsReadsEachVlanOnce(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { rdb.Close() })

	server.Select(ASICDB)
	vlans := map[string]string{"oid:0x2600000123": "506", "oid:0x2600000456": "777"}
	for bvid, vlanID := range vlans {
		server.HSet("ASIC_STATE:SAI_OBJECT_TYPE_VLAN:"+bvid, "SAI_VLAN_ATTR_VLAN_ID", vlanID)
	}

	ctx := context.Background()
	asic, err := openDB(ctx, rdb, ASICDB)
	if err != nil {
		t.Fatal(err)
	}
	defer asic.Close()

	fdbKeys := make([]fdbKey, 0, 500)
	for i := range 500 {
		bvid := "oid:0x2600000123"
		if i%2 == 0 {
			bvid = "oid:0x2600000456"
		}
		fdbKeys = append(fdbKeys, fdbKey{BvID: bvid, MAC: fmt.Sprintf("00:00:5E:00:%02X:%02X", i/256, i%256)})
	}

	before := server.CommandCount()
	vlanIDs, err := fdbVlanIDs(ctx, asic, fdbKeys)
	if err != nil {
		t.Fatal(err)
	}

	if reads := server.CommandCount() - before; reads != len(vlans) {
		t.Errorf("want one read per distinct VLAN OID (%d), got %d for %d entries", len(vlans), reads, len(fdbKeys))
	}
	if vlanIDs["oid:0x2600000123"] != 506 || vlanIDs["oid:0x2600000456"] != 777 {
		t.Errorf("wrong VLAN IDs: %v", vlanIDs)
	}
}

func TestParseFDBKey(t *testing.T) {
	key := `ASIC_STATE:SAI_OBJECT_TYPE_FDB_ENTRY:{"bvid":"oid:0x2600000123","mac":"00:00:5E:00:53:01","vlan":"506"}`

	parsed, err := parseFDBKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.BvID != "oid:0x2600000123" || parsed.MAC != "00:00:5E:00:53:01" || parsed.Vlan != "506" {
		t.Errorf("wrong key: %+v", parsed)
	}
	// the VLAN of the key wins over the OID to resolve
	if parsed.vlanKey() != "506" {
		t.Errorf("want the VLAN as lookup key, got %q", parsed.vlanKey())
	}

	noVlan := fdbKey{BvID: "oid:0x2600000123"}
	if noVlan.vlanKey() != "oid:0x2600000123" {
		t.Errorf("want the bridge VLAN OID as lookup key, got %q", noVlan.vlanKey())
	}

	for _, malformed := range []string{"ASIC_STATE:SAI_OBJECT_TYPE_FDB_ENTRY:not-json", "too-short"} {
		if _, err := parseFDBKey(malformed); err == nil {
			t.Errorf("%q should not parse", malformed)
		}
	}
}
