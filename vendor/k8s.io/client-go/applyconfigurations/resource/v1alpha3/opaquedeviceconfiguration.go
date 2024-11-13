/*
Copyright The Kubernetes Authors.

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

// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha3

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// OpaqueDeviceConfigurationApplyConfiguration represents a declarative configuration of the OpaqueDeviceConfiguration type for use
// with apply.
type OpaqueDeviceConfigurationApplyConfiguration struct {
	Driver     *string               `json:"driver,omitempty"`
	Parameters *runtime.RawExtension `json:"parameters,omitempty"`
}

// OpaqueDeviceConfigurationApplyConfiguration constructs a declarative configuration of the OpaqueDeviceConfiguration type for use with
// apply.
func OpaqueDeviceConfiguration() *OpaqueDeviceConfigurationApplyConfiguration {
	return &OpaqueDeviceConfigurationApplyConfiguration{}
}

// WithDriver sets the Driver field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Driver field is set to the value of the last call.
func (b *OpaqueDeviceConfigurationApplyConfiguration) WithDriver(value string) *OpaqueDeviceConfigurationApplyConfiguration {
	b.Driver = &value
	return b
}

// WithParameters sets the Parameters field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Parameters field is set to the value of the last call.
func (b *OpaqueDeviceConfigurationApplyConfiguration) WithParameters(value runtime.RawExtension) *OpaqueDeviceConfigurationApplyConfiguration {
	b.Parameters = &value
	return b
}
