package audit

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Cursor struct {
	Timestamp time.Time `json:"timestamp"`
	Seq       int64     `json:"seq"`
}

func EncodeCursor(cursor Cursor) (string, error) {
	if cursor.Timestamp.IsZero() || cursor.Seq <= 0 {
		return "", errors.New("invalid audit cursor")
	}
	raw, err := json.Marshal(Cursor{Timestamp: cursor.Timestamp.UTC(), Seq: cursor.Seq})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeCursor(value string) (Cursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Cursor{}, errors.New("invalid audit cursor encoding")
	}
	var cursor Cursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return Cursor{}, errors.New("invalid audit cursor payload")
	}
	if cursor.Timestamp.IsZero() || cursor.Seq <= 0 {
		return Cursor{}, errors.New("invalid audit cursor")
	}
	cursor.Timestamp = cursor.Timestamp.UTC()
	return cursor, nil
}
