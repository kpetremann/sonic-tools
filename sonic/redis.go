package sonic

import (
	"context"
	"errors"
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

// hGetEach reads one field of many keys in a single round-trip, in the order of the keys. A key
// which does not hold the field yields an empty string: the ASIC tables are read while the switch
// keeps writing them, so a key can disappear between the scan and the read.
func hGetEach(ctx context.Context, conn *redis.Conn, field string, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	cmds := make([]*redis.StringCmd, len(keys))
	_, err := conn.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for i, key := range keys {
			cmds[i] = pipe.HGet(ctx, key, field)
		}
		return nil
	})
	// Pipelined reports the first failing command, a missing key or field being one of them
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("failed to read %s of %d keys: %w", field, len(keys), err)
	}

	values := make([]string, len(keys))
	for i, cmd := range cmds {
		if value, err := cmd.Result(); err == nil {
			values[i] = value
		}
	}

	return values, nil
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
