package sonic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// platforms are the devices the fixtures in test_data come from:
//   - msn2700: NVIDIA MSN2700, SONiC 202211, FRR 8
//   - w6510: Micas W6510-32C, SONiC 202505, FRR 10
//
// The fixtures are anonymized dumps of real devices: addresses come from RFC 5737 and RFC 3849,
// AS numbers from RFC 6996, MAC addresses from RFC 7042 and host names from RFC 2606.
var platforms = []string{"msn2700", "w6510"}

func fixture(t *testing.T, platform, name string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("test_data", platform, name))
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

func fixtureJSON(t *testing.T, platform, name string, out any) {
	t.Helper()

	if err := json.Unmarshal([]byte(fixture(t, platform, name)), out); err != nil {
		t.Fatalf("%s/%s: %s", platform, name, err)
	}
}

func loadJSON(t *testing.T, path string, out any) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, out); err != nil {
		t.Fatalf("%s: %s", path, err)
	}
}

// TestJSONTagsAreSnakeCase keeps the document consumed by the validation tools consistent:
// every field published by the collection is named in snake_case, whatever the convention
// of the device, of FRR or of docker.
func TestJSONTagsAreSnakeCase(t *testing.T) {
	published := []any{
		Device{}, Platform{}, Version{}, Interface{}, PortConfig{}, Optic{}, OpticLane{},
		OpticThresholds{}, Counters{}, Neighbor{}, PSU{}, Fan{}, Temperature{}, BGPNeighbor{},
		SAFI{}, RouteMap{}, Rule{}, Container{}, IPTables{}, IPTablesFilter{}, IPTablesRule{},
		InterfaceAddr{}, IPNeighbor{}, ResolvedNeighbor{}, FDBEntry{},
	}
	snakeCase := regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

	for _, value := range published {
		structType := reflect.TypeOf(value)

		for i := range structType.NumField() {
			field := structType.Field(i)
			if field.Anonymous {
				continue // an embedded struct is flattened, its own fields are checked
			}

			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "" {
				t.Errorf("%s.%s has no json tag", structType.Name(), field.Name)
				continue
			}
			if !snakeCase.MatchString(name) {
				t.Errorf("%s.%s is published as %q, want snake_case", structType.Name(), field.Name, name)
			}
		}
	}
}
