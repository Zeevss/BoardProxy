package yandex

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

type Person struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	UserID      string `json:"userId,omitempty"`
	ProviderID  string `json:"providerId,omitempty"`
}

type Comment struct {
	ID        string    `json:"id"`
	PersonID  string    `json:"personId"`
	CreatedAt time.Time `json:"createdAt"`
	ParentID  string    `json:"parentId,omitempty"`
	Cell      string    `json:"cell"`
	Text      string    `json:"text"`
	Done      bool      `json:"done"`
}

type Thread struct {
	Root    Comment   `json:"root"`
	Replies []Comment `json:"replies"`
}

type legacyComment struct {
	Ref      string
	AuthorID int
	UID      string
	ShapeID  int
	Text     string
}

type vmlShape struct {
	ID     string
	Row    int
	Column int
}

type State struct {
	Persons        []Person
	Comments       []Comment
	LegacyAuthors  []string
	LegacyComments []legacyComment
	Shapes         []vmlShape
}

func (s *State) Threads(cell string) []Thread {
	cell = strings.ToUpper(strings.TrimSpace(cell))
	replies := make(map[string][]Comment)
	for _, comment := range s.Comments {
		if comment.ParentID != "" {
			replies[comment.ParentID] = append(replies[comment.ParentID], comment)
		}
	}

	var threads []Thread
	for _, comment := range s.Comments {
		if comment.ParentID != "" || (cell != "" && !strings.EqualFold(comment.Cell, cell)) {
			continue
		}
		threadReplies := append([]Comment(nil), replies[comment.ID]...)
		sort.SliceStable(threadReplies, func(i, j int) bool {
			return threadReplies[i].CreatedAt.Before(threadReplies[j].CreatedAt)
		})
		threads = append(threads, Thread{Root: comment, Replies: threadReplies})
	}
	sort.SliceStable(threads, func(i, j int) bool {
		return threads[i].Root.CreatedAt.Before(threads[j].Root.CreatedAt)
	})
	return threads
}

func (s *State) commentIndex(id string) int {
	for index := range s.Comments {
		if strings.EqualFold(s.Comments[index].ID, id) {
			return index
		}
	}
	return -1
}

func (s *State) personIndex(id string) int {
	for index := range s.Persons {
		if strings.EqualFold(s.Persons[index].ID, id) {
			return index
		}
	}
	return -1
}

func (s *State) legacyCommentIndex(rootID string) int {
	for index := range s.LegacyComments {
		if strings.EqualFold(s.LegacyComments[index].UID, rootID) {
			return index
		}
	}
	return -1
}

func (s *State) legacyAuthorIndex(rootID string) int {
	want := "tc=" + rootID
	for index, author := range s.LegacyAuthors {
		if strings.EqualFold(author, want) {
			return index
		}
	}
	return -1
}

func (s *State) thread(rootID string) (Thread, bool) {
	for _, thread := range s.Threads("") {
		if strings.EqualFold(thread.Root.ID, rootID) {
			return thread, true
		}
	}
	return Thread{}, false
}

type Operation struct {
	Type    string          `json:"t"`
	Path    []any           `json:"path"`
	Content json.RawMessage `json:"content,omitempty"`
	Prop    string          `json:"prop,omitempty"`
	Value   json.RawMessage `json:"v,omitempty"`
	UPh     any             `json:"uPh"`
}

type updateRequest struct {
	BundleID      int         `json:"bundleId"`
	Bundle        []Operation `json:"bundle"`
	BuildSnapshot bool        `json:"buildSnapshot"`
	Timeline      int         `json:"timeline"`
}

type serverBundle struct {
	ID       int         `json:"id"`
	UserID   int         `json:"userId"`
	Timeline int         `json:"timeline"`
	Bundle   []Operation `json:"bundle"`
}
