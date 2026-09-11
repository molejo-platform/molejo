package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

const (
	defaultPublicationPageSize = 50
	maximumPublicationPageSize = 100
	maximumPublicationCursor   = 512
)

var errPublicationCursor = errors.New("publication_cursor_invalid")

type publicationCursor struct {
	Kind string `json:"kind"`
	A    string `json:"a"`
	B    string `json:"b,omitempty"`
	C    string `json:"c,omitempty"`
}

func decodePublicationCursor(raw *string, kind string) (publicationCursor, error) {
	if raw == nil {
		return publicationCursor{Kind: kind}, nil
	}
	if len(*raw) == 0 || len(*raw) > maximumPublicationCursor {
		return publicationCursor{}, errPublicationCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(*raw)
	if err != nil || len(data) > maximumPublicationCursor {
		return publicationCursor{}, errPublicationCursor
	}
	var cursor publicationCursor
	if err = json.Unmarshal(data, &cursor); err != nil || cursor.Kind != kind || cursor.A == "" {
		return publicationCursor{}, errPublicationCursor
	}
	return cursor, nil
}

func encodePublicationCursor(cursor publicationCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

func publicationPageSize(limit *int) int {
	if limit == nil {
		return defaultPublicationPageSize
	}
	return *limit
}
