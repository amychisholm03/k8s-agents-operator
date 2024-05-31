/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"sync"
	"time"

	"github.com/go-logr/logr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/newrelic/k8s-agents-operator/src/autodetect"
	"github.com/newrelic/k8s-agents-operator/src/internal/version"
)

const (
	defaultAutoDetectFrequency = 5 * time.Second
)

// Config holds the static configuration for this operator.
type Config struct {
	autoDetect                     autodetect.AutoDetect
	logger                         logr.Logger
	autoInstrumentationPythonImage string
	autoInstrumentationDotNetImage string
	autoInstrumentationNodeJSImage string
	autoInstrumentationJavaImage   string
	autoInstrumentationGoImage     string
	autoInstrumentationPhpImage    string
	autoInstrumentationRubyImage   string
	onOpenShiftRoutesChange        changeHandler
	labelsFilter                   []string
	openshiftRoutes                openshiftRoutesStore
	autoDetectFrequency            time.Duration
	autoscalingVersion             autodetect.AutoscalingVersion
}

// New constructs a new configuration based on the given options.
func New(opts ...Option) Config {
	// initialize with the default values
	o := options{
		autoDetectFrequency:     defaultAutoDetectFrequency,
		logger:                  logf.Log.WithName("config"),
		openshiftRoutes:         newOpenShiftRoutesWrapper(),
		version:                 version.Get(),
		autoscalingVersion:      autodetect.DefaultAutoscalingVersion,
		onOpenShiftRoutesChange: newOnChange(),
	}
	for _, opt := range opts {
		opt(&o)
	}

	return Config{
		autoDetect:                     o.autoDetect,
		autoDetectFrequency:            o.autoDetectFrequency,
		logger:                         o.logger,
		openshiftRoutes:                o.openshiftRoutes,
		onOpenShiftRoutesChange:        o.onOpenShiftRoutesChange,
		autoInstrumentationJavaImage:   o.autoInstrumentationJavaImage,
		autoInstrumentationNodeJSImage: o.autoInstrumentationNodeJSImage,
		autoInstrumentationPythonImage: o.autoInstrumentationPythonImage,
		autoInstrumentationDotNetImage: o.autoInstrumentationDotNetImage,
		autoInstrumentationPhpImage:    o.autoInstrumentationPhpImage,
		autoInstrumentationRubyImage:   o.autoInstrumentationRubyImage,
		autoInstrumentationGoImage:     o.autoInstrumentationGoImage,
		labelsFilter:                   o.labelsFilter,
		autoscalingVersion:             o.autoscalingVersion,
	}
}

// StartAutoDetect attempts to automatically detect relevant information for this operator. This will block until the first
// run is executed and will schedule periodic updates.
func (c *Config) StartAutoDetect() error {
	err := c.AutoDetect()
	go c.periodicAutoDetect()

	return err
}

func (c *Config) periodicAutoDetect() {
	ticker := time.NewTicker(c.autoDetectFrequency)

	for range ticker.C {
		if err := c.AutoDetect(); err != nil {
			c.logger.Info("auto-detection failed", "error", err)
		}
	}
}

// AutoDetect attempts to automatically detect relevant information for this operator.
func (c *Config) AutoDetect() error {
	c.logger.V(2).Info("auto-detecting the configuration based on the environment")

	ora, err := c.autoDetect.OpenShiftRoutesAvailability()
	if err != nil {
		return err
	}

	if c.openshiftRoutes.Get() != ora {
		c.logger.V(1).Info("openshift routes detected", "available", ora)
		c.openshiftRoutes.Set(ora)
		if err = c.onOpenShiftRoutesChange.Do(); err != nil {
			// Don't fail if the callback failed, as auto-detection itself worked.
			c.logger.Error(err, "configuration change notification failed for callback")
		}
	}

	hpaVersion, err := c.autoDetect.HPAVersion()
	if err != nil {
		return err
	}
	c.autoscalingVersion = hpaVersion
	c.logger.V(2).Info("autoscaling version detected", "autoscaling-version", c.autoscalingVersion.String())

	return nil
}

// OpenShiftRoutes represents the availability of the OpenShift Routes API.
func (c *Config) OpenShiftRoutes() autodetect.OpenShiftRoutesAvailability {
	return c.openshiftRoutes.Get()
}

// AutoscalingVersion represents the preferred version of autoscaling.
func (c *Config) AutoscalingVersion() autodetect.AutoscalingVersion {
	return c.autoscalingVersion
}

// AutoInstrumentationJavaImage returns New Relic Java auto-instrumentation container image.
func (c *Config) AutoInstrumentationJavaImage() string {
	return c.autoInstrumentationJavaImage
}

// AutoInstrumentationNodeJSImage returns New Relic NodeJS auto-instrumentation container image.
func (c *Config) AutoInstrumentationNodeJSImage() string {
	return c.autoInstrumentationNodeJSImage
}

// AutoInstrumentationPythonImage returns New Relic Python auto-instrumentation container image.
func (c *Config) AutoInstrumentationPythonImage() string {
	return c.autoInstrumentationPythonImage
}

// AutoInstrumentationDotNetImage returns New Relic DotNet auto-instrumentation container image.
func (c *Config) AutoInstrumentationDotNetImage() string {
	return c.autoInstrumentationDotNetImage
}

// AutoInstrumentationDotNetImage returns New Relic DotNet auto-instrumentation container image.
func (c *Config) AutoInstrumentationPhpImage() string {
	return c.autoInstrumentationPhpImage
}

// AutoInstrumentationRubyImage returns New Relic Ruby auto-instrumentation container image.
func (c *Config) AutoInstrumentationRubyImage() string {
	return c.autoInstrumentationRubyImage
}

// AutoInstrumentationGoImage returns Opentelemtrey Go auto-instrumentation container image.
func (c *Config) AutoInstrumentationGoImage() string {
	return c.autoInstrumentationGoImage
}

// LabelsFilter Returns the filters converted to regex strings used to filter out unwanted labels from propagations.
func (c *Config) LabelsFilter() []string {
	return c.labelsFilter
}

// RegisterOpenShiftRoutesChangeCallback registers the given function as a callback that
// is called when the OpenShift Routes detection detects a change.
func (c *Config) RegisterOpenShiftRoutesChangeCallback(f func() error) {
	c.onOpenShiftRoutesChange.Register(f)
}

type openshiftRoutesStore interface {
	Set(ora autodetect.OpenShiftRoutesAvailability)
	Get() autodetect.OpenShiftRoutesAvailability
}

func newOpenShiftRoutesWrapper() openshiftRoutesStore {
	return &openshiftRoutesWrapper{
		current: autodetect.OpenShiftRoutesNotAvailable,
	}
}

type openshiftRoutesWrapper struct {
	mu      sync.Mutex
	current autodetect.OpenShiftRoutesAvailability
}

func (p *openshiftRoutesWrapper) Set(ora autodetect.OpenShiftRoutesAvailability) {
	p.mu.Lock()
	p.current = ora
	p.mu.Unlock()
}

func (p *openshiftRoutesWrapper) Get() autodetect.OpenShiftRoutesAvailability {
	p.mu.Lock()
	ora := p.current
	p.mu.Unlock()
	return ora
}
