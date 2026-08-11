package application

import (
	"context"
	"errors"

	"bproxy-control-plane/internal/domain"
	"bproxy-control-plane/internal/ports"
)

type DesiredStates struct {
	revisions ports.DesiredRevisions
	events    ports.DesiredNotifier
}

func NewDesiredStates(revisions ports.DesiredRevisions, events ports.DesiredNotifier) *DesiredStates {
	return &DesiredStates{revisions: revisions, events: events}
}

func (s *DesiredStates) Publish(ctx context.Context, nodeID string, expectedRevision, catalogVersion uint64, config []byte, cause string) (domain.ConfigRevision, error) {
	revision, err := s.revisions.AppendDesired(ctx, nodeID, expectedRevision, catalogVersion, config, cause)
	if err != nil {
		return domain.ConfigRevision{}, err
	}
	if s.events != nil {
		s.events.Notify(nodeID)
	}
	return revision, nil
}

func (s *DesiredStates) PublishNext(ctx context.Context, nodeID string, catalogVersion uint64, config []byte, cause string) (domain.ConfigRevision, error) {
	current, err := s.Get(ctx, nodeID)
	expected := uint64(0)
	if err == nil {
		expected = current.Revision
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.ConfigRevision{}, err
	}
	return s.Publish(ctx, nodeID, expected, catalogVersion, config, cause)
}

func (s *DesiredStates) Get(ctx context.Context, nodeID string) (domain.ConfigRevision, error) {
	return s.revisions.Desired(ctx, nodeID)
}

func (s *DesiredStates) History(ctx context.Context, nodeID string) ([]domain.ConfigRevision, error) {
	return s.revisions.DesiredHistory(ctx, nodeID)
}
