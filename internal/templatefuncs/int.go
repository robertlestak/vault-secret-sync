package templatefuncs

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	maxInt = int64(^uint(0) >> 1)
	minInt = -maxInt - 1
)

// Int converts common template input values to int while preserving errors for
// values that cannot be represented as an int.
func Int(v interface{}) (int, error) {
	switch value := v.(type) {
	case int:
		return value, nil
	case int8:
		return int(value), nil
	case int16:
		return int(value), nil
	case int32:
		return int(value), nil
	case int64:
		return intFromInt64(value)
	case uint:
		return intFromUint64(uint64(value))
	case uint8:
		return int(value), nil
	case uint16:
		return int(value), nil
	case uint32:
		return intFromUint64(uint64(value))
	case uint64:
		return intFromUint64(value)
	case float32:
		return intFromFloat64(float64(value))
	case float64:
		return intFromFloat64(value)
	case json.Number:
		return intFromString(value.String())
	case string:
		return intFromString(value)
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}

func intFromInt64(v int64) (int, error) {
	if v < minInt || v > maxInt {
		return 0, fmt.Errorf("integer %d overflows int", v)
	}
	return int(v), nil
}

func intFromUint64(v uint64) (int, error) {
	if v > uint64(maxInt) {
		return 0, fmt.Errorf("integer %d overflows int", v)
	}
	return int(v), nil
}

func intFromFloat64(v float64) (int, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("cannot convert %v to int", v)
	}
	if v != math.Trunc(v) {
		return 0, fmt.Errorf("number %v is not an integer", v)
	}
	if v < float64(minInt) || v >= math.Ldexp(1, strconv.IntSize-1) {
		return 0, fmt.Errorf("number %v overflows int", v)
	}
	return int(v), nil
}

func intFromString(v string) (int, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return 0, fmt.Errorf("cannot convert empty string to int")
	}
	if i, err := strconv.ParseInt(s, 10, 0); err == nil {
		return int(i), nil
	}
	if u, err := strconv.ParseUint(s, 10, 0); err == nil {
		return intFromUint64(u)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return intFromFloat64(f)
	}
	return 0, fmt.Errorf("cannot convert %q to int", v)
}
