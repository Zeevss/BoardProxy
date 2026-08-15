package yandex

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

func (s *State) apply(operations []Operation) error {
	for _, operation := range operations {
		if err := s.applyOne(operation); err != nil {
			return err
		}
	}
	return nil
}

func (s *State) applyOne(operation Operation) error {
	switch {
	case pathPrefix(operation.Path, "persons", "person") && operation.Type == "ie":
		var wire struct {
			ID, DisplayName, UserID, ProviderID string
		}
		if err := json.Unmarshal(operation.Content, &wire); err != nil {
			return err
		}
		return insertAt(&s.Persons, lastIndex(operation.Path), Person{
			ID: wire.ID, DisplayName: wire.DisplayName, UserID: wire.UserID, ProviderID: wire.ProviderID,
		})

	case pathContains(operation.Path, "threadedComments", "threadedComment"):
		index := lastIndex(operation.Path)
		switch operation.Type {
		case "ie":
			comment, err := decodeComment(operation.Content)
			if err != nil {
				return err
			}
			return insertAt(&s.Comments, index, comment)
		case "ue":
			if index < 0 || index >= len(s.Comments) {
				return fmt.Errorf("comment update index %d is out of range", index)
			}
			switch operation.Prop {
			case "text":
				var text struct {
					Value string `json:"v_"`
				}
				if err := json.Unmarshal(operation.Value, &text); err != nil {
					return err
				}
				s.Comments[index].Text = text.Value
			case "done":
				if err := json.Unmarshal(operation.Value, &s.Comments[index].Done); err != nil {
					return err
				}
			}
		case "de":
			return deleteAt(&s.Comments, index)
		}

	case pathContains(operation.Path, "comments", "authors", "author") && operation.Type == "ie":
		var author struct {
			Value string `json:"v_"`
		}
		if err := json.Unmarshal(operation.Content, &author); err != nil {
			return err
		}
		return insertAt(&s.LegacyAuthors, lastIndex(operation.Path), author.Value)

	case pathContains(operation.Path, "comments", "commentList", "comment"):
		index := lastIndex(operation.Path)
		if operation.Type == "ue" {
			parsed, err := strconv.Atoi(operation.Prop)
			if err != nil {
				return fmt.Errorf("invalid legacy comment property %q", operation.Prop)
			}
			index = parsed
		}
		switch operation.Type {
		case "ie":
			comment, err := decodeLegacyComment(operation.Content)
			if err != nil {
				return err
			}
			return insertAt(&s.LegacyComments, index, comment)
		case "ue":
			comment, err := decodeLegacyComment(operation.Value)
			if err != nil {
				return err
			}
			if index < 0 || index >= len(s.LegacyComments) {
				return fmt.Errorf("legacy comment update index %d is out of range", index)
			}
			s.LegacyComments[index] = comment
		case "de":
			return deleteAt(&s.LegacyComments, index)
		}

	case pathContains(operation.Path, "legacyDrawing", "vml", "shapes"):
		index := lastIndex(operation.Path)
		switch operation.Type {
		case "ie":
			var shape struct {
				ID         string `json:"id"`
				ClientData struct {
					Row struct {
						Content int `json:"content"`
					} `json:"row"`
					Column struct {
						Content int `json:"content"`
					} `json:"column"`
				} `json:"clientData"`
			}
			if err := json.Unmarshal(operation.Content, &shape); err != nil {
				return err
			}
			return insertAt(&s.Shapes, index, vmlShape{
				ID: shape.ID, Row: shape.ClientData.Row.Content, Column: shape.ClientData.Column.Content,
			})
		case "de":
			return deleteAt(&s.Shapes, index)
		}
	}
	return nil
}

func decodeComment(raw json.RawMessage) (Comment, error) {
	var wire struct {
		ID, PersonID, ParentID, Ref, DT string
		Done                            bool
		Text                            struct {
			Value string `json:"v_"`
		} `json:"text"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Comment{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, wire.DT)
	if err != nil {
		return Comment{}, err
	}
	return Comment{
		ID: wire.ID, PersonID: wire.PersonID, ParentID: wire.ParentID,
		Cell: wire.Ref, Text: wire.Text.Value, Done: wire.Done, CreatedAt: createdAt,
	}, nil
}

func decodeLegacyComment(raw json.RawMessage) (legacyComment, error) {
	var wire struct {
		Ref      string `json:"ref"`
		AuthorID int    `json:"authorId"`
		UID      string `json:"uid"`
		ShapeID  int    `json:"shapeId"`
		Text     struct {
			Text struct {
				Value string `json:"v_"`
			} `json:"t"`
		} `json:"text"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return legacyComment{}, err
	}
	return legacyComment{
		Ref: wire.Ref, AuthorID: wire.AuthorID, UID: wire.UID,
		ShapeID: wire.ShapeID, Text: wire.Text.Text.Value,
	}, nil
}

func pathPrefix(path []any, values ...string) bool {
	if len(path) < len(values) {
		return false
	}
	for index, value := range values {
		if path[index] != value {
			return false
		}
	}
	return true
}

func pathContains(path []any, values ...string) bool {
	if len(path) < len(values) {
		return false
	}
	for start := 0; start <= len(path)-len(values); start++ {
		matched := true
		for offset, value := range values {
			if path[start+offset] != value {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func lastIndex(path []any) int {
	if len(path) == 0 {
		return -1
	}
	switch value := path[len(path)-1].(type) {
	case int:
		return value
	case float64:
		return int(value)
	case json.Number:
		result, _ := strconv.Atoi(value.String())
		return result
	default:
		return -1
	}
}

func insertAt[T any](items *[]T, index int, value T) error {
	if index < 0 || index > len(*items) {
		return fmt.Errorf("insert index %d is out of range", index)
	}
	*items = append(*items, value)
	copy((*items)[index+1:], (*items)[index:len(*items)-1])
	(*items)[index] = value
	return nil
}

func deleteAt[T any](items *[]T, index int) error {
	if index < 0 || index >= len(*items) {
		return fmt.Errorf("delete index %d is out of range", index)
	}
	copy((*items)[index:], (*items)[index+1:])
	*items = (*items)[:len(*items)-1]
	return nil
}
