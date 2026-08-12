package sonic

import (
	"context"
	"fmt"
	"strings"

	redis "github.com/redis/go-redis/v9"
)

// NewRedis returns a client on the local SONiC Redis instance.
func NewRedis() *redis.Client {
	return redis.NewClient(&redis.Options{Addr: RedisAddr})
}

// openDB returns a connection bound to the given SONiC database, the caller must close it.
func openDB(ctx context.Context, rdb *redis.Client, db int) (*redis.Conn, error) {
	conn := rdb.Conn()
	if err := conn.Select(ctx, db).Err(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to select database %d: %w", db, err)
	}
	return conn, nil
}

func scanKeys(ctx context.Context, conn *redis.Conn, pattern string) ([]string, error) {
	keys := []string{}
	iter := conn.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan '%s': %w", pattern, err)
	}
	return keys, nil
}

// pick returns the first non empty field, several SONiC releases use different field names.
// The values of the transceiver tables are padded to the width of the EEPROM field.
func pick(fields map[string]string, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(fields[name]); value != "" {
			return value
		}
	}
	return ""
}
