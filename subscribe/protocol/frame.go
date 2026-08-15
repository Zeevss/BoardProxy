package protocol

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const framePrefix = "BP1 "

var ErrInvalidFrame = errors.New("invalid BoardProxy recovery frame")

type Frame struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	KeyID     string `json:"keyId"`
	Part      int    `json:"part"`
	Parts     int    `json:"parts"`
	Payload   []byte `json:"payload"`
}

func EncodeFrame(frame Frame) (string, error) {
	if err := frame.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		return "", err
	}
	return framePrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeFrame(text string) (Frame, error) {
	if !strings.HasPrefix(strings.TrimSpace(text), framePrefix) {
		return Frame{}, ErrInvalidFrame
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(text), framePrefix))
	if err != nil {
		return Frame{}, ErrInvalidFrame
	}
	var frame Frame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return Frame{}, ErrInvalidFrame
	}
	if err := frame.Validate(); err != nil {
		return Frame{}, err
	}
	return frame, nil
}

func (f Frame) Validate() error {
	if f.Type != "hello" && f.Type != "response" {
		return fmt.Errorf("%w: unsupported type", ErrInvalidFrame)
	}
	if f.RequestID == "" || f.KeyID == "" || f.Part < 1 || f.Parts < f.Part || f.Parts > 16 {
		return fmt.Errorf("%w: invalid metadata", ErrInvalidFrame)
	}
	if len(f.Payload) == 0 || len(f.Payload) > 64<<10 {
		return fmt.Errorf("%w: invalid payload size", ErrInvalidFrame)
	}
	return nil
}
