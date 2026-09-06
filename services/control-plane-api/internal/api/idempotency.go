package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func idempotency(r *http.Request) (string, []byte, bool) {
	value := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if value == "" || len(value) > 128 {
		return "", nil, false
	}
	var body map[string]any
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
	if err != nil {
		return "", nil, false
	}
	r.Body = io.NopCloser(strings.NewReader(string(raw)))
	_ = json.Unmarshal(raw, &body)
	canonical, _ := json.Marshal(body)
	return value, domain.SHA256(canonical), true
}

func scopedRequestPayloadHash(r *http.Request, bodyHash []byte) []byte {
	value := append([]byte(r.Method+"\n"+r.URL.EscapedPath()+"\n"), bodyHash...)
	return domain.SHA256(value)
}
