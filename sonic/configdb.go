package sonic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

func RunningConfigDB(ctx context.Context) (map[string]any, error) {
	config := map[string]any{}
	if err := runJSON(ctx, &config, "show", "runningconfiguration", "all"); err != nil {
		return nil, err
	}
	return config, nil
}

func StartupConfigDB() (map[string]any, error) {
	data, err := os.ReadFile(ConfigDBFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", ConfigDBFile, err)
	}

	config := map[string]any{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", ConfigDBFile, err)
	}

	return config, nil
}

func SaveConfigDB() (string, error) {
	return runDetached("config", "save", "-y")
}
