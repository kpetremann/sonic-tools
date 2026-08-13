package sonic

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

func SNMPConfig() (map[string]any, error) {
	data, err := os.ReadFile(SNMPFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", SNMPFile, err)
	}

	config := map[string]any{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", SNMPFile, err)
	}

	return config, nil
}
