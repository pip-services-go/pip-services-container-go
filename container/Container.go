package container

import (
	"errors"

	cconfig "github.com/pip-services3-go/pip-services3-commons-go/config"
	cconv "github.com/pip-services3-go/pip-services3-commons-go/convert"
	cerr "github.com/pip-services3-go/pip-services3-commons-go/errors"
	crefer "github.com/pip-services3-go/pip-services3-commons-go/refer"
	cbuild "github.com/pip-services3-go/pip-services3-components-go/build"
	"github.com/pip-services3-go/pip-services3-components-go/info"
	"github.com/pip-services3-go/pip-services3-components-go/log"
	"github.com/pip-services3-go/pip-services3-container-go/build"
	"github.com/pip-services3-go/pip-services3-container-go/config"
	"github.com/pip-services3-go/pip-services3-container-go/refer"
)

/*
Inversion of control (IoC) container that creates components and manages their lifecycle.

The container is driven by configuration, that usually stored in JSON or YAML file. The configuration contains a list of components identified by type or locator, followed by component configuration.

On container start it performs the following actions:

Creates components using their types or calls registered factories to create components using their locators
Configures components that implement IConfigurable interface and passes them their configuration parameters
Sets references to components that implement IReferenceable interface and passes them references of all components in the container
Opens components that implement IOpenable interface
On container stop actions are performed in reversed order:

Closes components that implement ICloseable interface
Unsets references in components that implement IUnreferenceable interface
Destroys components in the container.
The component configuration can be parameterized by dynamic values. That allows specialized containers to inject parameters from command line or from environment variables.

The container automatically creates a ContextInfo component that carries detail information about the container and makes it available for other components.

see
IConfigurable (in the PipServices "Commons" package)

see
IReferenceable (in the PipServices "Commons" package)

see
IOpenable (in the PipServices "Commons" package)

Configuration parameters
name: the context (container or process) name
description: human-readable description of the context
properties: entire section of additional descriptive properties
 - ...
Example
  ======= config.yml ========
  - descriptor: mygroup:mycomponent1:default:default:1.0
    param1: 123
    param2: ABC

  - type: mycomponent2,mypackage
    param1: 321
    param2: XYZ
  ============================

  container := NewEmptyContainer()
  container.AddFactory(newMyComponentFactory())

  parameters := NewConfigParamsFromValue(process.env)
  container.ReadConfigFromFile("123", "./config/config.yml", parameters)

  err := container.Open("123")
  ftm.Println("Container is opened")
  ...
  err = container.Close("123")
  fmt.Println("Container is closed")
*/
type Container struct {
	logger          log.ILogger
	factories       *cbuild.CompositeFactory
	info            *info.ContextInfo
	config          config.ContainerConfig
	references      *refer.ContainerReferences
	referenceable   crefer.IReferenceable
	unreferenceable crefer.IUnreferenceable
}

// Creates a new empty instance of the container.
// Returns *Container
func NewEmptyContainer() *Container {
	return &Container{
		logger:    log.NewNullLogger(),
		factories: build.NewDefaultContainerFactory(),
		info:      info.NewContextInfo(),
	}
}

// Creates a new instance of the container.
// Parameters:
//  - name string
//  a container name (accessible via ContextInfo)
//  - description string
//  a container description (accessible via ContextInfo)
// Returns *Container
func NewContainer(name string, description string) *Container {
	c := NewEmptyContainer()

	c.info.Name = name
	c.info.Description = description

	return c
}

// Creates a new instance of the container inherit from reference.
// Parameters:
//   - name string
//   a container name (accessible via ContextInfo)
//   - description string
//   a container description (accessible via ContextInfo)
//   - referenceable crefer.IReferenceable
//   - referenceble object for inherit
// Returns *Container
func InheritContainer(name string, description string,
	referenceable crefer.IReferenceable) *Container {
	c := NewEmptyContainer()

	c.info.Name = name
	c.info.Description = description
	c.referenceable = referenceable
	c.unreferenceable, _ = referenceable.(crefer.IUnreferenceable)

	return c
}

// Configures component by passing configuration parameters.
// Parameters:
//   - config  *cconfig.ConfigParams
//   configuration parameters to be set.
func (c *Container) Configure(conf *cconfig.ConfigParams) {
	c.config, _ = config.ReadContainerConfigFromConfig(conf)
}

// Reads container configuration from JSON or YAML file and parameterizes it with given values.
// Parameters:
//   - correlationId string
//   transaction id to trace execution through call chain.
//   - path string
//   a path to configuration file
//   - parameters *cconfig.ConfigParams
// values to parameters the configuration or null to skip parameterization.
func (c *Container) ReadConfigFromFile(correlationId string,
	path string, parameters *cconfig.ConfigParams) error {

	var err error
	c.config, err = config.ContainerConfigReader.ReadFromFile(correlationId, path, parameters)
	//c.logger.Trace(correlationId, config.String())
	return err
}

func (c *Container) initReferences(references crefer.IReferences) {
	existingInfo, ok := references.GetOneOptional(
		crefer.NewDescriptor("pip-services", "context-info", "*", "*", "1.0"),
	).(*info.ContextInfo)
	if !ok {
		references.Put(
			crefer.NewDescriptor("pip-services", "context-info", "default", "default", "1.0"),
			c.info,
		)
	} else {
		c.info = existingInfo
	}

	references.Put(
		crefer.NewDescriptor("pip-services", "factory", "container", "default", "1.0"),
		c.factories,
	)
}

func (c *Container) Logger() log.ILogger {
	return c.logger
}

func (c *Container) SetLogger(logger log.ILogger) {
	c.logger = logger
}

func (c *Container) Info() *info.ContextInfo {
	return c.info
}

// Adds a factory to the container. The factory is used to create components added to the container by their locators (descriptors).
// Parameters:
//  - factory IFactory
//  a component factory to be added.
func (c *Container) AddFactory(factory cbuild.IFactory) {
	c.factories.Add(factory)
}

// Checks if the component is opened.
// Returns bool
// true if the component has been opened and false otherwise.
func (c *Container) IsOpen() bool {
	return c.references != nil
}

// Opens the component.
// Parameters:
//   - correlationId string
//   transaction id to trace execution through call chain.
// Returns error
func (c *Container) Open(correlationId string) (err error) {
	//var err error

	if c.references != nil {
		return cerr.NewInvalidStateError(
			correlationId, "ALREADY_OPENED", "Container was already opened",
		)
	}

	defer func() {
		if r := recover(); r != nil {
			recoverErr, ok := r.(error)
			if !ok {
				msg, _ := r.(string)
				recoverErr = errors.New(msg)
			}
			err = recoverErr
			c.logger.Error(correlationId, recoverErr, "Failed to start container")
			c.Close(correlationId)
		}
	}()

	c.logger.Trace(correlationId, "Starting container.")

	// Create references with configured components
	c.references = refer.NewContainerReferences()
	c.initReferences(c.references)
	err = c.references.PutFromConfig(c.config)
	if err != nil {
		return err
	}

	if c.referenceable != nil {
		c.referenceable.SetReferences(c.references)
	}

	// Get custom description if available
	infoDescriptor := crefer.NewDescriptor("*", "context-info", "*", "*", "*")
	info, ok := c.references.GetOneOptional(infoDescriptor).(*info.ContextInfo)
	if ok {
		c.info = info
	}

	// Get reference to logger
	c.logger = log.NewCompositeLoggerFromReferences(c.references)

	// Open references
	err = c.references.Open(correlationId)
	if err == nil {
		c.logger.Info(correlationId, "Container %s started", c.info.Name)
	} else {
		c.logger.Fatal(correlationId, err, "Failed to start container")
		c.Close(correlationId)
	}

	return err
}

// Closes component and frees used resources.
// Parameters:
//   - correlationId string
//   transaction id to trace execution through call chain.
// Returns error
func (c *Container) Close(correlationId string) error {
	// Skip if container wasn't opened
	if c.references == nil {
		return nil
	}

	var err error

	defer func() {
		if r := recover(); r != nil {
			err, ok := r.(error)
			if !ok {
				msg := cconv.StringConverter.ToString(r)
				err = errors.New(msg)
			}
			c.logger.Error(correlationId, err, "Failed to stop container")
		}
	}()

	c.logger.Trace(correlationId, "Stopping %s container", c.info.Name)

	// Unset references for child container
	if c.unreferenceable != nil {
		c.unreferenceable.UnsetReferences()
	}

	// Close and dereference components
	err = c.references.Close(correlationId)

	c.references = nil

	if err == nil {
		c.logger.Info(correlationId, "Container %s stopped", c.info.Name)
	} else {
		c.logger.Error(correlationId, err, "Failed to stop container")
	}

	return err
}
