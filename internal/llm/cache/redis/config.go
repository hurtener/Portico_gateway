package redis

import (
	"strconv"
	"time"
)

// strOpt reads a string config key with a default. Non-string values are
// ignored (defensive: yaml-decoded config can surface unexpected types).
func strOpt(cfg map[string]any, key, def string) string {
	v, ok := cfg[key]
	if !ok {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

// intOpt reads a positive-integer config key with a default. Accepts int,
// int64, float64, and numeric strings (mirrors inmem's helper).
func intOpt(cfg map[string]any, key string, def int) int {
	v, ok := cfg[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case int:
		if t >= 0 {
			return t
		}
	case int64:
		if t >= 0 {
			return int(t)
		}
	case float64:
		if t >= 0 {
			return int(t)
		}
	case string:
		if n, err := strconv.Atoi(t); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

// durOpt reads a duration config key, falling back to defaultTTL. Accepts a
// duration string ("5m"), a bare time.Duration, or a numeric value in seconds.
func durOpt(cfg map[string]any, key string) time.Duration {
	v, ok := cfg[key]
	if !ok {
		return defaultTTL
	}
	switch t := v.(type) {
	case time.Duration:
		if t > 0 {
			return t
		}
	case string:
		if d, err := time.ParseDuration(t); err == nil && d > 0 {
			return d
		}
	case int:
		if t > 0 {
			return time.Duration(t) * time.Second
		}
	case float64:
		if t > 0 {
			return time.Duration(t) * time.Second
		}
	}
	return defaultTTL
}
