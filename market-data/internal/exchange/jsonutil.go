package exchange

import (
	"encoding/json"
	"strconv"
	"time"
)

// StringFloat unmarshals a JSON number given either as a quoted string
// ("63043.40", used by Bybit/OKX) or a raw number (used by some Binance
// fields), so collector structs don't need per-exchange parsing code.
type StringFloat float64

func (f *StringFloat) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		*f = StringFloat(v)
		return nil
	}
	var v float64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*f = StringFloat(v)
	return nil
}

// StringInt64 mirrors StringFloat for millisecond epoch timestamps, which
// Bybit/OKX quote as strings and Binance sends as raw numbers.
type StringInt64 int64

func (i *StringInt64) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*i = StringInt64(v)
		return nil
	}
	var v int64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*i = StringInt64(v)
	return nil
}

func (i StringInt64) Time() time.Time {
	return time.UnixMilli(int64(i))
}
