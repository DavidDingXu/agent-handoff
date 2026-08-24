package bundle

import (
	"bytes"
	"encoding/json"
	"sort"
)

func marshalIndent(v any) []byte {
	if v == nil {
		v = map[string]any{}
	}
	data, _ := json.MarshalIndent(v, "", "  ")
	return append(data, '\n')
}

func unmarshalStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	return dec.Decode(v)
}

func unmarshalMap(data []byte) map[string]any {
	m := map[string]any{}
	if len(data) > 0 {
		_ = unmarshalStrict(data, &m)
	}
	return m
}

func sortStrings(s []string) { sort.Strings(s) }
