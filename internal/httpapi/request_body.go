package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
)

func bodyFromMap(payload map[string]any) io.ReadCloser {
	data, _ := json.Marshal(payload)
	return io.NopCloser(bytes.NewReader(data))
}
