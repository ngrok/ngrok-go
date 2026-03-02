package ngrok

import (
	"context"
	"errors"
)

// UpdateEndpointOption is a functional option for updating an endpoint.
type UpdateEndpointOption func(*updateEndpointOpts)

type updateEndpointOpts struct {
	description    *string
	metadata       *string
	trafficPolicy  *string
	poolingEnabled *bool
}

// WithUpdateDescription sets a new description for the endpoint.
func WithUpdateDescription(desc string) UpdateEndpointOption {
	return func(opts *updateEndpointOpts) {
		opts.description = &desc
	}
}

// WithUpdateMetadata sets new metadata for the endpoint.
func WithUpdateMetadata(meta string) UpdateEndpointOption {
	return func(opts *updateEndpointOpts) {
		opts.metadata = &meta
	}
}

// WithUpdateTrafficPolicy sets a new traffic policy for the endpoint.
func WithUpdateTrafficPolicy(policy string) UpdateEndpointOption {
	return func(opts *updateEndpointOpts) {
		opts.trafficPolicy = &policy
	}
}

// WithUpdatePoolingEnabled sets whether pooling is enabled.
func WithUpdatePoolingEnabled(enabled bool) UpdateEndpointOption {
	return func(opts *updateEndpointOpts) {
		opts.poolingEnabled = &enabled
	}
}

// Update modifies the endpoint's mutable fields. Nil values are not changed.
func (e *baseEndpoint) Update(ctx context.Context, opts ...UpdateEndpointOption) error {
	updateOpts := &updateEndpointOpts{}
	for _, opt := range opts {
		opt(updateOpts)
	}

	a, ok := e.agent.(*agent)
	if !ok {
		return errors.New("agent type assertion failed")
	}

	a.mu.RLock()
	sess := a.sess
	a.mu.RUnlock()

	if sess == nil {
		return errors.New("agent not connected")
	}

	if err := sess.UpdateBind(e.id, updateOpts.description, updateOpts.metadata, updateOpts.trafficPolicy, updateOpts.poolingEnabled); err != nil {
		return wrapError(err)
	}

	// Update local state
	if updateOpts.description != nil {
		e.description = *updateOpts.description
	}
	if updateOpts.metadata != nil {
		e.metadata = *updateOpts.metadata
	}
	if updateOpts.trafficPolicy != nil {
		e.trafficPolicy = *updateOpts.trafficPolicy
	}
	if updateOpts.poolingEnabled != nil {
		e.poolingEnabled = *updateOpts.poolingEnabled
	}

	return nil
}
