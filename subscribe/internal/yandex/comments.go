package yandex

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotFound     = errors.New("comment or thread not found")
	ErrInvalidCell  = errors.New("invalid A1 cell reference")
	ErrEmptyText    = errors.New("comment text is empty")
	ErrThreadExists = errors.New("the cell already has a comment thread")
)

const legacyWarning = "[Threaded comment]\n\n" +
	"Your application allows you to read this threaded comment; however, any edits to it will get removed if the file is opened in a latest version of Excel.\n\n"

// Threads returns an immutable view of all folders, optionally filtered by cell.
// An empty cell returns folders from the whole first worksheet.
func (c *Client) Threads(cell string) []Thread {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.State.Threads(cell)
}

// CreateThread creates a folder at a cell. Its first note is the root comment.
func (c *Client) CreateThread(ctx context.Context, cell, text string) (Thread, error) {
	row, column, normalized, err := parseCell(cell)
	if err != nil {
		return Thread{}, err
	}
	if strings.TrimSpace(text) == "" {
		return Thread{}, ErrEmptyText
	}

	var rootID string
	err = c.mutate(ctx, func() ([]Operation, error) {
		if len(c.State.Threads(normalized)) != 0 {
			return nil, ErrThreadExists
		}
		person, personOperation, err := c.personForCurrentUser()
		if err != nil {
			return nil, err
		}
		rootID, err = newGUID()
		if err != nil {
			return nil, err
		}
		root := Comment{
			ID: rootID, PersonID: person.ID, CreatedAt: time.Now().UTC(),
			Cell: normalized, Text: text,
		}
		authorIndex := len(c.State.LegacyAuthors)
		legacyIndex := len(c.State.LegacyComments)
		shapeIndex := len(c.State.Shapes)
		shapeID, err := c.newShapeID()
		if err != nil {
			return nil, err
		}
		legacy := legacyComment{
			Ref: normalized, AuthorID: authorIndex, UID: rootID,
			ShapeID: 0, Text: flattenThread(Thread{Root: root}),
		}

		operations := make([]Operation, 0, 5)
		if personOperation != nil {
			operations = append(operations, *personOperation)
		}
		operations = append(operations,
			insertOperation(
				[]any{"worksheet", 0, "threadedComments", "threadedComment", len(c.State.Comments)},
				commentWire(root),
			),
			insertOperation(
				[]any{"worksheet", 0, "comments", "authors", "author", authorIndex},
				map[string]any{"t_": "author", "v_": "tc=" + rootID},
			),
			insertOperation(
				[]any{"worksheet", 0, "comments", "commentList", "comment", legacyIndex},
				legacyWire(legacy),
			),
			insertOperation(
				[]any{"worksheet", 0, "legacyDrawing", "vml", "shapes", shapeIndex},
				shapeWire(shapeID, row, column),
			),
		)
		return operations, nil
	})
	if err != nil {
		return Thread{}, err
	}
	var created Thread
	for _, thread := range c.Threads(normalized) {
		if strings.EqualFold(thread.Root.ID, rootID) {
			created = thread
			break
		}
	}
	if created.Root.ID == "" {
		return Thread{}, fmt.Errorf("created thread %s is absent from local state", rootID)
	}
	return created, nil
}

// AddReply adds a note to an existing folder.
func (c *Client) AddReply(ctx context.Context, rootID, text string) (Comment, error) {
	if strings.TrimSpace(text) == "" {
		return Comment{}, ErrEmptyText
	}
	var replyID string
	err := c.mutate(ctx, func() ([]Operation, error) {
		thread, ok := c.State.thread(rootID)
		if !ok {
			return nil, ErrNotFound
		}
		person, personOperation, err := c.personForCurrentUser()
		if err != nil {
			return nil, err
		}
		replyID, err = newGUID()
		if err != nil {
			return nil, err
		}
		reply := Comment{
			ID: replyID, PersonID: person.ID, ParentID: thread.Root.ID,
			CreatedAt: time.Now().UTC(), Cell: thread.Root.Cell, Text: text,
		}
		thread.Replies = append(thread.Replies, reply)
		legacyIndex := c.State.legacyCommentIndex(thread.Root.ID)
		if legacyIndex < 0 {
			return nil, fmt.Errorf("%w: legacy mirror for thread %s", ErrNotFound, rootID)
		}
		legacy := c.State.LegacyComments[legacyIndex]
		legacy.Text = flattenThread(thread)
		operations := make([]Operation, 0, 3)
		if personOperation != nil {
			operations = append(operations, *personOperation)
		}
		operations = append(operations,
			insertOperation(
				[]any{"worksheet", 0, "threadedComments", "threadedComment", len(c.State.Comments)},
				commentWire(reply),
			),
			updateLegacyOperation(legacyIndex, legacy),
		)
		return operations, nil
	})
	if err != nil {
		return Comment{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	index := c.State.commentIndex(replyID)
	if index < 0 {
		return Comment{}, fmt.Errorf("created reply %s is absent from local state", replyID)
	}
	return c.State.Comments[index], nil
}

// EditComment edits either the root note or a reply and rebuilds its legacy mirror.
func (c *Client) EditComment(ctx context.Context, commentID, text string) error {
	if strings.TrimSpace(text) == "" {
		return ErrEmptyText
	}
	return c.mutate(ctx, func() ([]Operation, error) {
		index := c.State.commentIndex(commentID)
		if index < 0 {
			return nil, ErrNotFound
		}
		comment := c.State.Comments[index]
		if comment.Text == text {
			return nil, nil
		}
		rootID := comment.ParentID
		if rootID == "" {
			rootID = comment.ID
		}
		thread, ok := c.State.thread(rootID)
		if !ok {
			return nil, ErrNotFound
		}
		if strings.EqualFold(thread.Root.ID, comment.ID) {
			thread.Root.Text = text
		} else {
			for replyIndex := range thread.Replies {
				if strings.EqualFold(thread.Replies[replyIndex].ID, comment.ID) {
					thread.Replies[replyIndex].Text = text
				}
			}
		}
		legacyIndex := c.State.legacyCommentIndex(rootID)
		if legacyIndex < 0 {
			return nil, fmt.Errorf("%w: legacy mirror for thread %s", ErrNotFound, rootID)
		}
		legacy := c.State.LegacyComments[legacyIndex]
		legacy.Text = flattenThread(thread)
		return []Operation{
			updateOperation(
				[]any{"worksheet", 0, "threadedComments", "threadedComment", index},
				"text", map[string]any{"t_": "sc_text", "v_": text},
			),
			updateLegacyOperation(legacyIndex, legacy),
		}, nil
	})
}

// ResolveThread toggles the folder's resolved state.
func (c *Client) ResolveThread(ctx context.Context, rootID string, done bool) error {
	return c.mutate(ctx, func() ([]Operation, error) {
		index := c.State.commentIndex(rootID)
		if index < 0 || c.State.Comments[index].ParentID != "" {
			return nil, ErrNotFound
		}
		if c.State.Comments[index].Done == done {
			return nil, nil
		}
		return []Operation{updateOperation(
			[]any{"worksheet", 0, "threadedComments", "threadedComment", index}, "done", done,
		)}, nil
	})
}

// DeleteComment deletes one reply. Passing a root ID deletes the whole folder.
func (c *Client) DeleteComment(ctx context.Context, commentID string) error {
	return c.mutate(ctx, func() ([]Operation, error) {
		index := c.State.commentIndex(commentID)
		if index < 0 {
			return nil, ErrNotFound
		}
		comment := c.State.Comments[index]
		if comment.ParentID == "" {
			return c.deleteThreadOperations(comment.ID)
		}

		thread, ok := c.State.thread(comment.ParentID)
		if !ok {
			return nil, ErrNotFound
		}
		filtered := thread.Replies[:0:0]
		for _, reply := range thread.Replies {
			if !strings.EqualFold(reply.ID, comment.ID) {
				filtered = append(filtered, reply)
			}
		}
		thread.Replies = filtered
		legacyIndex := c.State.legacyCommentIndex(thread.Root.ID)
		if legacyIndex < 0 {
			return nil, fmt.Errorf("%w: legacy mirror for thread %s", ErrNotFound, thread.Root.ID)
		}
		legacy := c.State.LegacyComments[legacyIndex]
		legacy.Text = flattenThread(thread)
		return []Operation{
			deleteOperation([]any{"worksheet", 0, "threadedComments", "threadedComment", index}),
			updateLegacyOperation(legacyIndex, legacy),
		}, nil
	})
}

func (c *Client) deleteThreadOperations(rootID string) ([]Operation, error) {
	thread, ok := c.State.thread(rootID)
	if !ok {
		return nil, ErrNotFound
	}
	indexes := make([]int, 0, len(thread.Replies)+1)
	indexes = append(indexes, c.State.commentIndex(thread.Root.ID))
	for _, reply := range thread.Replies {
		indexes = append(indexes, c.State.commentIndex(reply.ID))
	}
	sort.Sort(sort.Reverse(sort.IntSlice(indexes)))
	operations := make([]Operation, 0, len(indexes)+2)
	for _, index := range indexes {
		operations = append(operations, deleteOperation(
			[]any{"worksheet", 0, "threadedComments", "threadedComment", index},
		))
	}
	legacyIndex := c.State.legacyCommentIndex(rootID)
	if legacyIndex < 0 {
		return nil, fmt.Errorf("%w: legacy mirror for thread %s", ErrNotFound, rootID)
	}
	shapeIndex := -1
	cellRow, cellColumn, _, err := parseCell(thread.Root.Cell)
	if err == nil {
		for index, shape := range c.State.Shapes {
			if shape.Row == cellRow && shape.Column == cellColumn {
				shapeIndex = index
				break
			}
		}
	}
	if shapeIndex < 0 && legacyIndex < len(c.State.Shapes) {
		shapeIndex = legacyIndex
	}
	if shapeIndex < 0 {
		return nil, fmt.Errorf("%w: VML shape for thread %s", ErrNotFound, rootID)
	}
	operations = append(operations,
		deleteOperation([]any{"worksheet", 0, "legacyDrawing", "vml", "shapes", shapeIndex}),
		deleteOperation([]any{"worksheet", 0, "comments", "commentList", "comment", legacyIndex}),
	)
	return operations, nil
}

func (c *Client) mutate(ctx context.Context, build func() ([]Operation, error)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	operations, err := build()
	if err != nil {
		return err
	}
	if len(operations) == 0 {
		return nil
	}
	err = c.sendUpdate(ctx, operations)
	if !errors.Is(err, ErrConflict) {
		return err
	}
	if err := c.syncLocked(ctx); err != nil {
		return fmt.Errorf("synchronize after conflict: %w", err)
	}
	operations, err = build()
	if err != nil {
		return err
	}
	if len(operations) == 0 {
		return nil
	}
	return c.sendUpdate(ctx, operations)
}

func (c *Client) personForCurrentUser() (Person, *Operation, error) {
	for _, person := range c.State.Persons {
		if c.config.WOPIUserID != "" && person.UserID == c.config.WOPIUserID {
			return person, nil, nil
		}
		if c.config.Anonymous && person.UserID == "" &&
			(c.currentUserName == "" || person.DisplayName == c.currentUserName) {
			return person, nil, nil
		}
	}
	displayName := strings.TrimSpace(c.currentUserName)
	if displayName == "" {
		if c.config.Anonymous {
			displayName = "Гость"
		} else {
			displayName = "Yandex Sheets user"
		}
	}
	id, err := newGUID()
	if err != nil {
		return Person{}, nil, err
	}
	person := Person{
		ID: id, DisplayName: displayName, ProviderID: "None",
	}
	if !c.config.Anonymous {
		person.UserID = c.config.WOPIUserID
	}
	operation := insertOperation(
		[]any{"persons", "person", len(c.State.Persons)},
		map[string]any{
			"t_": "sc_person", "id": person.ID, "displayName": person.DisplayName,
			"userId": person.UserID, "providerId": person.ProviderID,
		},
	)
	return person, &operation, nil
}

func parseCell(cell string) (row, column int, normalized string, err error) {
	cell = strings.ToUpper(strings.TrimSpace(cell))
	if cell == "" || strings.ContainsAny(cell, "!:$") {
		return 0, 0, "", ErrInvalidCell
	}
	lettersEnd := 0
	for lettersEnd < len(cell) && cell[lettersEnd] >= 'A' && cell[lettersEnd] <= 'Z' {
		lettersEnd++
	}
	if lettersEnd == 0 || lettersEnd == len(cell) {
		return 0, 0, "", ErrInvalidCell
	}
	rowNumber, parseErr := strconv.Atoi(cell[lettersEnd:])
	if parseErr != nil || rowNumber < 1 || rowNumber > 1_048_576 {
		return 0, 0, "", ErrInvalidCell
	}
	columnNumber := 0
	for _, letter := range cell[:lettersEnd] {
		columnNumber = columnNumber*26 + int(letter-'A'+1)
	}
	if columnNumber < 1 || columnNumber > 16_384 {
		return 0, 0, "", ErrInvalidCell
	}
	return rowNumber - 1, columnNumber - 1, cell, nil
}

func newGUID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate GUID: %w", err)
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	hexValue := strings.ToUpper(hex.EncodeToString(bytes))
	return fmt.Sprintf("{%s-%s-%s-%s-%s}",
		hexValue[0:8], hexValue[8:12], hexValue[12:16], hexValue[16:20], hexValue[20:32],
	), nil
}

func (c *Client) newShapeID() (string, error) {
	for attempts := 0; attempts < 10; attempts++ {
		bytes := make([]byte, 4)
		if _, err := rand.Read(bytes); err != nil {
			return "", fmt.Errorf("generate VML shape ID: %w", err)
		}
		number := 100_000 + (int(bytes[0])<<16|int(bytes[1])<<8|int(bytes[2]))%900_000
		candidate := "\x00s" + strconv.Itoa(number)
		unique := true
		for _, shape := range c.State.Shapes {
			if shape.ID == candidate {
				unique = false
				break
			}
		}
		if unique {
			return candidate, nil
		}
	}
	return "", errors.New("cannot allocate unique VML shape ID")
}

func commentWire(comment Comment) map[string]any {
	result := map[string]any{
		"t_": "sc_threadedComment", "id": comment.ID, "personId": comment.PersonID,
		"dT":  comment.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		"ref": comment.Cell, "text": map[string]any{"t_": "sc_text", "v_": comment.Text},
	}
	if comment.ParentID != "" {
		result["parentId"] = comment.ParentID
	}
	if comment.Done {
		result["done"] = true
	}
	return result
}

func legacyWire(comment legacyComment) map[string]any {
	return map[string]any{
		"t_": "comment", "ref": comment.Ref, "authorId": comment.AuthorID,
		"uid": comment.UID, "shapeId": comment.ShapeID,
		"text": map[string]any{
			"t_": "text", "t": map[string]any{"t_": "t", "v_": comment.Text},
		},
	}
}

func shapeWire(id string, row, column int) map[string]any {
	return map[string]any{
		"t_": "v_shape", "id": id, "type": "#\x00t202",
		"style":     "position:absolute;width:108pt;height:59.25pt;visibility:hidden;z-index:1",
		"fillcolor": "#f2f3cb", "strokecolor": "#81835a", "insetmode": "auto",
		"fill": map[string]any{
			"t_": "v_fill", "color2": "#fefefb", "type": "gradient",
			"fill": map[string]any{"t_": "o_fill", "ext": "view", "type": "gradientUnscaled"},
		},
		"shadow": map[string]any{
			"t_": "v_shadow", "on": true, "color": "silver", "opacity": "0.5", "obscured": true,
		},
		"pathElement": map[string]any{"t_": "v_path", "connecttype": "none"},
		"clientData": map[string]any{
			"t_": "x_ClientData", "ObjectType": "Note",
			"moveWithCells": map[string]any{"t_": "x_MoveWithCells"},
			"sizeWithCells": map[string]any{"t_": "x_SizeWithCells"},
			"autoFill":      map[string]any{"t_": "x_AutoFill", "content": false},
			"row":           map[string]any{"t_": "x_Row", "content": row},
			"column":        map[string]any{"t_": "x_Column", "content": column},
		},
	}
}

func flattenThread(thread Thread) string {
	var text strings.Builder
	text.WriteString(legacyWarning)
	text.WriteString("Comment:\n\t\t")
	text.WriteString(thread.Root.Text)
	text.WriteString("\n\n")
	for _, reply := range thread.Replies {
		text.WriteString("Reply:\n\t\t")
		text.WriteString(reply.Text)
		text.WriteByte('\n')
	}
	return text.String()
}

func insertOperation(path []any, content any) Operation {
	return Operation{Type: "ie", Path: path, Content: mustJSON(content), UPh: nil}
}

func updateOperation(path []any, property string, value any) Operation {
	return Operation{Type: "ue", Path: path, Prop: property, Value: mustJSON(value), UPh: nil}
}

func deleteOperation(path []any) Operation {
	return Operation{Type: "de", Path: path, UPh: nil}
}

func updateLegacyOperation(index int, comment legacyComment) Operation {
	return updateOperation(
		[]any{"worksheet", 0, "comments", "commentList", "comment"},
		strconv.Itoa(index), legacyWire(comment),
	)
}

func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
