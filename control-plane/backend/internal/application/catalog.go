package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"bproxy-control-plane/internal/domain"
	"bproxy-control-plane/internal/ports"
)

type Catalogs struct {
	store    ports.CatalogStore
	compiler ports.ConfigCompiler
	desired  *DesiredStates
	statuses ports.NodeStatusStore
	audit    ports.AuditLog
	now      func() time.Time
	newID    func() string
}

type MutationResult struct {
	Catalog       domain.Catalog
	Desired       domain.ConfigRevision
	ConfigChanged bool
}

func NewCatalogs(store ports.CatalogStore, compiler ports.ConfigCompiler, desired *DesiredStates, statuses ports.NodeStatusStore, audit ports.AuditLog) *Catalogs {
	return &Catalogs{store: store, compiler: compiler, desired: desired, statuses: statuses, audit: audit, now: time.Now, newID: randomID}
}

func (s *Catalogs) Create(ctx context.Context, catalog domain.Catalog, actor string) (MutationResult, error) {
	if actor == "" {
		return MutationResult{}, errors.New("catalogs: actor is required")
	}
	if err := catalog.Validate(); err != nil {
		return MutationResult{}, err
	}
	if catalog.Version != 1 {
		return MutationResult{}, fmt.Errorf("catalogs: new catalog version must be 1")
	}
	if err := s.store.SaveCatalog(ctx, catalog, 0); err != nil {
		return MutationResult{}, err
	}
	if err := s.appendAudit(ctx, catalog, actor, "catalog.created", "catalog", catalog.Node.ID, catalog.Version); err != nil {
		return MutationResult{}, err
	}
	return s.reconcile(ctx, catalog, "catalog.created")
}

func (s *Catalogs) ReplaceNode(ctx context.Context, nodeID string, candidate domain.Node, expectedVersion uint64, actor string) (MutationResult, error) {
	return s.mutate(ctx, nodeID, actor, func(catalog domain.Catalog) (domain.Catalog, string, string, uint64, error) {
		next, err := catalog.ReplaceNode(candidate, expectedVersion, s.now())
		return next, "node.updated", candidate.ID, next.Node.Version, err
	})
}

func (s *Catalogs) ReplaceBoard(ctx context.Context, nodeID string, candidate domain.Board, expectedVersion uint64, actor string) (MutationResult, error) {
	return s.mutate(ctx, nodeID, actor, func(catalog domain.Catalog) (domain.Catalog, string, string, uint64, error) {
		next, err := catalog.ReplaceBoard(candidate, expectedVersion, s.now())
		version := uint64(0)
		if err == nil {
			for _, board := range next.Boards {
				if board.ID == candidate.ID {
					version = board.Version
				}
			}
		}
		return next, "board.updated", candidate.ID, version, err
	})
}

func (s *Catalogs) ReplaceUser(ctx context.Context, nodeID string, candidate domain.User, expectedVersion uint64, actor string) (MutationResult, error) {
	return s.mutate(ctx, nodeID, actor, func(catalog domain.Catalog) (domain.Catalog, string, string, uint64, error) {
		next, err := catalog.ReplaceUser(candidate, expectedVersion, s.now())
		version := uint64(0)
		if err == nil {
			for _, user := range next.Users {
				if user.ID == candidate.ID {
					version = user.Version
				}
			}
		}
		return next, "user.updated", candidate.ID, version, err
	})
}

func (s *Catalogs) ReplaceAssignment(ctx context.Context, nodeID string, candidate domain.NodeAssignment, expectedVersion uint64, actor string) (MutationResult, error) {
	return s.mutate(ctx, nodeID, actor, func(catalog domain.Catalog) (domain.Catalog, string, string, uint64, error) {
		next, err := catalog.ReplaceAssignment(candidate, expectedVersion, s.now())
		return next, "assignment.updated", candidate.NodeID, next.Assignment.Version, err
	})
}

type catalogMutation func(domain.Catalog) (domain.Catalog, string, string, uint64, error)

func (s *Catalogs) mutate(ctx context.Context, nodeID, actor string, mutation catalogMutation) (MutationResult, error) {
	if actor == "" {
		return MutationResult{}, errors.New("catalogs: actor is required")
	}
	catalog, err := s.store.Catalog(ctx, nodeID)
	if err != nil {
		return MutationResult{}, err
	}
	next, action, resourceID, resourceVersion, err := mutation(catalog)
	if err != nil {
		return MutationResult{}, err
	}
	if err := s.store.SaveCatalog(ctx, next, catalog.Version); err != nil {
		return MutationResult{}, err
	}
	resourceType := action
	for index, value := range resourceType {
		if value == '.' {
			resourceType = resourceType[:index]
			break
		}
	}
	if err := s.appendAudit(ctx, next, actor, action, resourceType, resourceID, resourceVersion); err != nil {
		return MutationResult{}, err
	}
	return s.reconcile(ctx, next, action)
}

func (s *Catalogs) Reconcile(ctx context.Context, nodeID, cause string) (MutationResult, error) {
	catalog, err := s.store.Catalog(ctx, nodeID)
	if err != nil {
		return MutationResult{}, err
	}
	return s.reconcile(ctx, catalog, cause)
}

func (s *Catalogs) reconcile(ctx context.Context, catalog domain.Catalog, cause string) (MutationResult, error) {
	config, err := s.compiler.Compile(catalog)
	if err != nil {
		return MutationResult{}, err
	}
	current, err := s.desired.Get(ctx, catalog.Node.ID)
	expectedRevision := uint64(0)
	if err == nil {
		expectedRevision = current.Revision
		digest := sha256.Sum256(config)
		if current.ConfigSHA256 == hex.EncodeToString(digest[:]) {
			if err := s.projectDesiredRevision(ctx, catalog.Node.ID, current.Revision); err != nil {
				return MutationResult{}, err
			}
			return MutationResult{Catalog: catalog, Desired: current}, nil
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return MutationResult{}, err
	}
	revision, err := s.desired.Publish(ctx, catalog.Node.ID, expectedRevision, catalog.Version, config, cause)
	if err != nil {
		return MutationResult{}, err
	}
	if err := s.projectDesiredRevision(ctx, catalog.Node.ID, revision.Revision); err != nil {
		return MutationResult{}, err
	}
	return MutationResult{Catalog: catalog, Desired: revision, ConfigChanged: true}, nil
}

func (s *Catalogs) ReconcileAll(ctx context.Context, cause string) error {
	catalogs, err := s.store.ListCatalogs(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, catalog := range catalogs {
		if _, err := s.reconcile(ctx, catalog, cause); err != nil {
			failures = append(failures, fmt.Errorf("node %s: %w", catalog.Node.ID, err))
		}
	}
	return errors.Join(failures...)
}

func (s *Catalogs) projectDesiredRevision(ctx context.Context, nodeID string, revision uint64) error {
	return updateNodeStatus(ctx, s.statuses, nodeID, func(status *domain.NodeStatus) bool {
		if status.DesiredRevision == revision {
			return false
		}
		status.DesiredRevision = revision
		return true
	})
}

func (s *Catalogs) appendAudit(ctx context.Context, catalog domain.Catalog, actor, action, resourceType, resourceID string, resourceVersion uint64) error {
	return s.audit.AppendAudit(ctx, domain.AuditEvent{
		ID: s.newID(), NodeID: catalog.Node.ID, Actor: actor, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, ResourceVersion: resourceVersion,
		CatalogVersion: catalog.Version, OccurredAt: s.now().UTC(),
	})
}

func randomID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("event-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}
