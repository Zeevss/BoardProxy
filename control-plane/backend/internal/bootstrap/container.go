package bootstrap

import (
	"bproxy-control-plane/internal/adapters/coreconfig"
	"bproxy-control-plane/internal/adapters/events"
	"bproxy-control-plane/internal/adapters/filesystem"
	"bproxy-control-plane/internal/adapters/pki"
	"bproxy-control-plane/internal/application"
)

type Container struct {
	Repository *filesystem.Store
	Authority  *pki.Authority
	Enrollment *application.Enrollment
	Desired    *application.DesiredStates
	Catalogs   *application.Catalogs
	Events     *events.Bus
}

func Build(dataDirectory string, serverNames []string) (*Container, error) {
	repository, err := filesystem.Open(dataDirectory)
	if err != nil {
		return nil, err
	}
	authority, err := pki.Open(dataDirectory, serverNames)
	if err != nil {
		return nil, err
	}
	eventBus := events.New()
	desired := application.NewDesiredStates(repository, eventBus)
	return &Container{
		Repository: repository, Authority: authority,
		Enrollment: application.NewEnrollment(repository, authority),
		Desired:    desired, Events: eventBus,
		Catalogs: application.NewCatalogs(repository, coreconfig.Compiler{}, desired, repository, repository),
	}, nil
}
