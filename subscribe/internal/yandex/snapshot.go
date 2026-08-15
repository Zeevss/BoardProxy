package yandex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type xmlNode struct {
	Type        string          `json:"t_"`
	Children    []xmlNode       `json:"c_"`
	Value       string          `json:"v_"`
	ID          string          `json:"id"`
	DisplayName string          `json:"displayName"`
	UserID      string          `json:"userId"`
	ProviderID  string          `json:"providerId"`
	PersonID    string          `json:"personId"`
	ParentID    string          `json:"parentId"`
	DateTime    string          `json:"dT"`
	Ref         string          `json:"ref"`
	AuthorID    json.RawMessage `json:"authorId"`
	ShapeID     json.RawMessage `json:"shapeId"`
	UID         string          `json:"xr:uid"`
	Done        json.RawMessage `json:"done"`
	ObjectType  string          `json:"ObjectType"`
}

func parseSnapshot(data []byte) (State, error) {
	var parts map[string]json.RawMessage
	if err := json.Unmarshal(data, &parts); err != nil {
		return State{}, fmt.Errorf("decode main.json: %w", err)
	}

	var state State
	if err := decodePart(parts, "xl/persons/person.xml", func(root xmlNode) {
		for _, child := range root.Children {
			state.Persons = append(state.Persons, Person{
				ID: child.ID, DisplayName: child.DisplayName,
				UserID: child.UserID, ProviderID: child.ProviderID,
			})
		}
	}); err != nil {
		return State{}, err
	}

	if err := decodePart(parts, "xl/threadedComments/threadedComment1.xml", func(root xmlNode) {
		for _, child := range root.Children {
			createdAt, _ := time.Parse(time.RFC3339Nano, child.DateTime)
			state.Comments = append(state.Comments, Comment{
				ID: child.ID, PersonID: child.PersonID, ParentID: child.ParentID,
				CreatedAt: createdAt, Cell: child.Ref, Text: firstValue(child),
				Done: parseFlexibleBool(child.Done),
			})
		}
	}); err != nil {
		return State{}, err
	}

	if err := decodePart(parts, "xl/comments1.xml", func(root xmlNode) {
		for _, section := range root.Children {
			switch section.Type {
			case "s:authors":
				for _, author := range section.Children {
					state.LegacyAuthors = append(state.LegacyAuthors, author.Value)
				}
			case "s:commentList":
				for _, comment := range section.Children {
					state.LegacyComments = append(state.LegacyComments, legacyComment{
						Ref: comment.Ref, AuthorID: parseFlexibleInt(comment.AuthorID),
						UID: comment.UID, ShapeID: parseFlexibleInt(comment.ShapeID),
						Text: firstValue(comment),
					})
				}
			}
		}
	}); err != nil {
		return State{}, err
	}

	if err := decodePart(parts, "xl/drawings/vmlDrawing1.vml", func(root xmlNode) {
		for _, shape := range root.Children {
			if shape.Type != "v:shape" {
				continue
			}
			item := vmlShape{ID: shape.ID}
			for _, child := range shape.Children {
				if child.Type != "x:ClientData" {
					continue
				}
				for _, property := range child.Children {
					switch property.Type {
					case "x:Row":
						item.Row, _ = strconv.Atoi(property.Value)
					case "x:Column":
						item.Column, _ = strconv.Atoi(property.Value)
					}
				}
			}
			state.Shapes = append(state.Shapes, item)
		}
	}); err != nil {
		return State{}, err
	}

	return state, nil
}

func decodePart(parts map[string]json.RawMessage, name string, consume func(xmlNode)) error {
	raw, ok := parts[name]
	if !ok || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var root xmlNode
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("decode snapshot part %s: %w", name, err)
	}
	consume(root)
	return nil
}

func firstValue(node xmlNode) string {
	if node.Value != "" {
		return node.Value
	}
	for _, child := range node.Children {
		if value := firstValue(child); value != "" {
			return value
		}
	}
	return ""
}

func parseFlexibleInt(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var number int
	if json.Unmarshal(raw, &number) == nil {
		return number
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		number, _ = strconv.Atoi(text)
	}
	return number
}

func parseFlexibleBool(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var value bool
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		text = strings.ToLower(text)
		return text == "1" || text == "true"
	}
	return false
}
