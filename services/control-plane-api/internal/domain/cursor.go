package domain

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
)

func EncodeCursor(id int64) string {
	if id < 1 {
		return ""
	}
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], uint64(id))
	return base64.RawURLEncoding.EncodeToString(value[:])
}

func DecodeCursor(cursor string) (int64, error) {
	value, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(value) != 8 {
		return 0, errors.New("invalid cursor")
	}
	id := int64(binary.BigEndian.Uint64(value))
	if id < 1 {
		return 0, errors.New("invalid cursor")
	}
	return id, nil
}
