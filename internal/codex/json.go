package codex

import (
	"encoding/json"
	"time"
)

func jsonUnmarshal(data string, v any) error {
	return json.Unmarshal([]byte(data), v)
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// parseIndexTime is a lenient RFC3339 parser for index timestamps; 0 on error.
func parseIndexTime(s string) int64 {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}
