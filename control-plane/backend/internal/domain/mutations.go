package domain

import (
	"fmt"
	"time"
)

func NewCatalog(node Node, boards []Board, users []User, assignment NodeAssignment, now time.Time) (Catalog, error) {
	now = now.UTC()
	node.Version, node.UpdatedAt = 1, now
	for index := range boards {
		boards[index].Version, boards[index].UpdatedAt = 1, now
	}
	for index := range users {
		users[index].Version, users[index].UpdatedAt = 1, now
	}
	assignment.Version, assignment.UpdatedAt = 1, now
	catalog := Catalog{
		Node: node, Boards: append([]Board(nil), boards...), Users: append([]User(nil), users...),
		Assignment: assignment, Version: 1, UpdatedAt: now,
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func (c Catalog) ReplaceNode(candidate Node, expectedVersion uint64, now time.Time) (Catalog, error) {
	if candidate.ID != c.Node.ID {
		return Catalog{}, invalid("node ID is immutable")
	}
	if c.Node.Version != expectedVersion {
		return Catalog{}, versionConflict("node", candidate.ID, expectedVersion, c.Node.Version)
	}
	if err := allowStateTransition("node", candidate.ID, c.Node.State, candidate.State); err != nil {
		return Catalog{}, err
	}
	candidate.Version = c.Node.Version + 1
	candidate.UpdatedAt = now.UTC()
	next := c.clone()
	next.Node = candidate
	return next.finishMutation(now)
}

func (c Catalog) ReplaceBoard(candidate Board, expectedVersion uint64, now time.Time) (Catalog, error) {
	next := c.clone()
	index := boardIndex(next.Boards, candidate.ID)
	currentVersion := uint64(0)
	if index >= 0 {
		currentVersion = next.Boards[index].Version
	}
	if currentVersion != expectedVersion {
		return Catalog{}, versionConflict("board", candidate.ID, expectedVersion, currentVersion)
	}
	if index >= 0 {
		if err := allowStateTransition("board", candidate.ID, next.Boards[index].State, candidate.State); err != nil {
			return Catalog{}, err
		}
	}
	candidate.Version = currentVersion + 1
	candidate.UpdatedAt = now.UTC()
	if index < 0 {
		next.Boards = append(next.Boards, candidate)
	} else {
		next.Boards[index] = candidate
	}
	return next.finishMutation(now)
}

func (c Catalog) ReplaceUser(candidate User, expectedVersion uint64, now time.Time) (Catalog, error) {
	next := c.clone()
	index := userIndex(next.Users, candidate.ID)
	currentVersion := uint64(0)
	if index >= 0 {
		currentVersion = next.Users[index].Version
	}
	if currentVersion != expectedVersion {
		return Catalog{}, versionConflict("user", candidate.ID, expectedVersion, currentVersion)
	}
	if index >= 0 {
		if err := allowStateTransition("user", candidate.ID, next.Users[index].State, candidate.State); err != nil {
			return Catalog{}, err
		}
	}
	candidate.Version = currentVersion + 1
	candidate.UpdatedAt = now.UTC()
	if index < 0 {
		next.Users = append(next.Users, candidate)
	} else {
		next.Users[index] = candidate
	}
	return next.finishMutation(now)
}

func (c Catalog) ReplaceAssignment(candidate NodeAssignment, expectedVersion uint64, now time.Time) (Catalog, error) {
	if candidate.NodeID != c.Node.ID {
		return Catalog{}, invalid("assignment node ID must match catalog node")
	}
	if c.Assignment.Version != expectedVersion {
		return Catalog{}, versionConflict("assignment", candidate.NodeID, expectedVersion, c.Assignment.Version)
	}
	next := c.clone()
	candidate.Version = c.Assignment.Version + 1
	candidate.UpdatedAt = now.UTC()
	candidate.BoardIDs = append([]string(nil), candidate.BoardIDs...)
	candidate.Users = cloneAssignedUsers(candidate.Users)
	next.Assignment = candidate
	return next.finishMutation(now)
}

func (c Catalog) finishMutation(now time.Time) (Catalog, error) {
	c.Version++
	c.UpdatedAt = now.UTC()
	if err := c.Validate(); err != nil {
		return Catalog{}, err
	}
	return c, nil
}

func (c Catalog) clone() Catalog {
	c.Boards = append([]Board(nil), c.Boards...)
	c.Users = append([]User(nil), c.Users...)
	c.Assignment.BoardIDs = append([]string(nil), c.Assignment.BoardIDs...)
	c.Assignment.Users = cloneAssignedUsers(c.Assignment.Users)
	return c
}

func cloneAssignedUsers(values []AssignedUser) []AssignedUser {
	cloned := make([]AssignedUser, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].BoardIDs = append([]string(nil), value.BoardIDs...)
	}
	return cloned
}

func boardIndex(boards []Board, id string) int {
	for index := range boards {
		if boards[index].ID == id {
			return index
		}
	}
	return -1
}

func userIndex(users []User, id string) int {
	for index := range users {
		if users[index].ID == id {
			return index
		}
	}
	return -1
}

func versionConflict(resource, id string, expected, actual uint64) error {
	return fmt.Errorf("%w: %s %q expected version %d, actual %d", ErrConflict, resource, id, expected, actual)
}

func allowStateTransition(resource, id string, current, next ResourceState) error {
	if current == ResourceRevoked && next != ResourceRevoked {
		return invalid("%s %q is revoked and cannot be restored", resource, id)
	}
	return nil
}
