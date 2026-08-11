package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"bproxy-control-plane/internal/adapters/coreconfig"
	"bproxy-control-plane/internal/adapters/events"
	"bproxy-control-plane/internal/adapters/filesystem"
	"bproxy-control-plane/internal/application"
	"bproxy-control-plane/internal/domain"
)

type catalogSeed struct {
	Node       domain.Node           `json:"node"`
	Boards     []domain.Board        `json:"boards"`
	Users      []domain.User         `json:"users"`
	Assignment domain.NodeAssignment `json:"assignment"`
}

func (a App) catalog(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: bproxy-hub catalog <seed|node|board|user|assignment|reconcile|history>")
	}
	switch args[0] {
	case "seed":
		return a.seedCatalog(args[1:])
	case "node", "board", "user", "assignment":
		return a.mutateCatalogResource(args[0], args[1:])
	case "reconcile":
		return a.reconcileCatalog(args[1:])
	case "history":
		return a.catalogHistory(args[1:])
	default:
		return fmt.Errorf("unknown catalog command %q", args[0])
	}
}

func (a App) seedCatalog(args []string) error {
	flags := flag.NewFlagSet("catalog seed", flag.ContinueOnError)
	data := flags.String("data", "/var/lib/bproxy-hub", "persistent hub data directory")
	path := flags.String("file", "", "catalog JSON path or - for stdin")
	actor := flags.String("actor", "cli", "audit actor")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("--file is required")
	}
	var input catalogSeed
	if err := decodeJSONInput(*path, a.Stdin, &input); err != nil {
		return err
	}
	catalog, err := domain.NewCatalog(input.Node, input.Boards, input.Users, input.Assignment, time.Now().UTC())
	if err != nil {
		return err
	}
	_, catalogs, err := openCatalogApplication(*data)
	if err != nil {
		return err
	}
	result, err := catalogs.Create(context.Background(), catalog, *actor)
	if err != nil {
		return err
	}
	return writeMutationResult(a.Stdout, result)
}

func (a App) mutateCatalogResource(kind string, args []string) error {
	flags := flag.NewFlagSet("catalog "+kind, flag.ContinueOnError)
	data := flags.String("data", "/var/lib/bproxy-hub", "persistent hub data directory")
	nodeID := flags.String("node", "", "node id")
	path := flags.String("file", "", "resource JSON path or - for stdin")
	expected := flags.Uint64("expected-version", 0, "current resource version; zero creates board/user")
	actor := flags.String("actor", "cli", "audit actor")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *nodeID == "" || *path == "" {
		return errors.New("--node and --file are required")
	}
	_, catalogs, err := openCatalogApplication(*data)
	if err != nil {
		return err
	}
	ctx := context.Background()
	var result application.MutationResult
	switch kind {
	case "node":
		var value domain.Node
		if err := decodeJSONInput(*path, a.Stdin, &value); err != nil {
			return err
		}
		result, err = catalogs.ReplaceNode(ctx, *nodeID, value, *expected, *actor)
	case "board":
		var value domain.Board
		if err := decodeJSONInput(*path, a.Stdin, &value); err != nil {
			return err
		}
		result, err = catalogs.ReplaceBoard(ctx, *nodeID, value, *expected, *actor)
	case "user":
		var value domain.User
		if err := decodeJSONInput(*path, a.Stdin, &value); err != nil {
			return err
		}
		result, err = catalogs.ReplaceUser(ctx, *nodeID, value, *expected, *actor)
	case "assignment":
		var value domain.NodeAssignment
		if err := decodeJSONInput(*path, a.Stdin, &value); err != nil {
			return err
		}
		result, err = catalogs.ReplaceAssignment(ctx, *nodeID, value, *expected, *actor)
	}
	if err != nil {
		return err
	}
	return writeMutationResult(a.Stdout, result)
}

func (a App) reconcileCatalog(args []string) error {
	flags := flag.NewFlagSet("catalog reconcile", flag.ContinueOnError)
	data := flags.String("data", "/var/lib/bproxy-hub", "persistent hub data directory")
	nodeID := flags.String("node", "", "node id")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *nodeID == "" {
		return errors.New("--node is required")
	}
	_, catalogs, err := openCatalogApplication(*data)
	if err != nil {
		return err
	}
	result, err := catalogs.Reconcile(context.Background(), *nodeID, "cli.reconcile")
	if err != nil {
		return err
	}
	return writeMutationResult(a.Stdout, result)
}

func (a App) catalogHistory(args []string) error {
	flags := flag.NewFlagSet("catalog history", flag.ContinueOnError)
	data := flags.String("data", "/var/lib/bproxy-hub", "persistent hub data directory")
	nodeID := flags.String("node", "", "node id")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *nodeID == "" {
		return errors.New("--node is required")
	}
	store, _, err := openCatalogApplication(*data)
	if err != nil {
		return err
	}
	history, err := store.DesiredHistory(context.Background(), *nodeID)
	if err != nil {
		return err
	}
	metadata := make([]map[string]any, 0, len(history))
	for _, revision := range history {
		metadata = append(metadata, map[string]any{
			"revision": revision.Revision, "previous_revision": revision.PreviousRevision,
			"catalog_version": revision.CatalogVersion, "config_sha256": revision.ConfigSHA256,
			"cause": revision.Cause, "updated_at": revision.UpdatedAt,
		})
	}
	encoder := json.NewEncoder(a.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(metadata)
}

func openCatalogApplication(dataDirectory string) (*filesystem.Store, *application.Catalogs, error) {
	store, err := filesystem.Open(dataDirectory)
	if err != nil {
		return nil, nil, err
	}
	bus := events.New()
	desired := application.NewDesiredStates(store, bus)
	return store, application.NewCatalogs(store, coreconfig.Compiler{}, desired, store, store), nil
}

func decodeJSONInput(path string, stdin io.Reader, target any) error {
	reader := stdin
	if path != "-" {
		file, err := readInput(path, stdin)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(file)
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode catalog JSON: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("decode catalog JSON: trailing data")
	}
	return nil
}

func writeMutationResult(writer io.Writer, result application.MutationResult) error {
	_, err := fmt.Fprintf(writer, "node %s catalog_version %d desired_revision %d sha256 %s changed %t\n",
		result.Catalog.Node.ID, result.Catalog.Version, result.Desired.Revision,
		result.Desired.ConfigSHA256, result.ConfigChanged,
	)
	return err
}
