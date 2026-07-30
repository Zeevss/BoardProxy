package yandex

import (
	"encoding/json"

	"bproxy-core/internal/board"
)

// textStyle is the mxGraph style used for the textbox objects that carry our
// payload. The exact style is cosmetic; what matters is that it is a valid
// textbox so the board accepts and persists it (SPEC §5.4).
const textStyle = "text;html=1;strokeColor=none;fillColor=none;align=left;" +
	"verticalAlign=middle;whiteSpace=wrap;rounded=0;fontSize=12;fontStyle=0;"

// cellAttrs mirrors the mxCell `_attributes_` map. All values are strings.
type cellAttrs struct {
	ID                 string `json:"id"`
	Value              string `json:"value"`
	Style              string `json:"style"`
	Vertex             string `json:"vertex"`
	Type               string `json:"type"`
	CreatorHash        string `json:"creatorHash"`
	Parent             string `json:"parent"`
	Index              string `json:"index"`
	WidthExpandAllowed string `json:"widthExpandAllowed"`
}

type geomAttrs struct {
	X      string `json:"x"`
	Y      string `json:"y"`
	Width  string `json:"width"`
	Height string `json:"height"`
	As     string `json:"as"`
}

type mxGeometry struct {
	Attributes geomAttrs `json:"_attributes_"`
}

// mxCell is one whiteboard object as sent to / received from the board.
type mxCell struct {
	Attributes cellAttrs    `json:"_attributes_"`
	Geometry   []mxGeometry `json:"mxGeometry"`
	Hash       string       `json:"hash"`
}

// buildCell wraps a board.Object into a textbox mxCell ready for modify-objects.
func buildCell(obj board.Object) mxCell {
	return mxCell{
		Attributes: cellAttrs{
			ID:                 obj.ID,
			Value:              obj.Value,
			Style:              textStyle,
			Vertex:             "1",
			Type:               "textbox",
			CreatorHash:        obj.CreatorHash,
			Parent:             "DASHBOARD",
			Index:              "1",
			WidthExpandAllowed: "1",
		},
		Geometry: []mxGeometry{{Attributes: geomAttrs{
			X: "0", Y: "0", Width: "40", Height: "20", As: "geometry",
		}}},
		Hash: obj.ID,
	}
}

// parseCell converts a raw mxCell (from a snapshot or broadcast) into a
// board.Object. ok is false if the blob is not a usable object (no id).
func parseCell(raw json.RawMessage) (board.Object, bool) {
	var c mxCell
	if err := json.Unmarshal(raw, &c); err != nil {
		return board.Object{}, false
	}
	id := c.Attributes.ID
	if id == "" {
		id = c.Hash
	}
	if id == "" {
		return board.Object{}, false
	}
	return board.Object{
		ID:          id,
		Value:       c.Attributes.Value,
		CreatorHash: c.Attributes.CreatorHash,
	}, true
}

// newObjectID returns a fresh board object id. Object identity lives in the
// board package so the driver and link layer agree on the format (SPEC §5.4).
func newObjectID() string { return board.NewID() }
