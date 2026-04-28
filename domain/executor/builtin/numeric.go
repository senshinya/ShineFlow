package builtin

import (
	"encoding/json"
	"strconv"
)

// coerceFloat64 尝试把输入转换为 float64，供 if/switch 数值比较复用。
func coerceFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f, true
		}
	case string:
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

type integerValue struct {
	signed bool
	i      int64
	u      uint64
}

func (v integerValue) equal(other integerValue) bool {
	if v.signed && other.signed {
		return v.i == other.i
	}
	if !v.signed && !other.signed {
		return v.u == other.u
	}
	if v.signed {
		return v.i >= 0 && uint64(v.i) == other.u
	}
	return other.i >= 0 && v.u == uint64(other.i)
}

func exactInteger(v any) (integerValue, bool) {
	switch x := v.(type) {
	case int:
		return integerValue{signed: true, i: int64(x)}, true
	case int8:
		return integerValue{signed: true, i: int64(x)}, true
	case int16:
		return integerValue{signed: true, i: int64(x)}, true
	case int32:
		return integerValue{signed: true, i: int64(x)}, true
	case int64:
		return integerValue{signed: true, i: x}, true
	case uint:
		return integerValue{u: uint64(x)}, true
	case uint8:
		return integerValue{u: uint64(x)}, true
	case uint16:
		return integerValue{u: uint64(x)}, true
	case uint32:
		return integerValue{u: uint64(x)}, true
	case uint64:
		return integerValue{u: x}, true
	case json.Number:
		return parseExactIntegerString(x.String())
	case string:
		return parseExactIntegerString(x)
	}
	return integerValue{}, false
}

func parseExactIntegerString(s string) (integerValue, bool) {
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return integerValue{signed: true, i: i}, true
	}
	if u, err := strconv.ParseUint(s, 10, 64); err == nil {
		return integerValue{u: u}, true
	}
	return integerValue{}, false
}
