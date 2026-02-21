package registration

// NoopBroadcaster is a no-operation broadcaster for single-instance deployments.
type NoopBroadcaster struct{}

// NewNoopBroadcaster creates a no-op broadcaster.
func NewNoopBroadcaster() *NoopBroadcaster {
	return &NoopBroadcaster{}
}

// Publish is a no-op.
func (b *NoopBroadcaster) Publish(_ string, _ []byte) error {
	return nil
}

// Subscribe is a no-op.
func (b *NoopBroadcaster) Subscribe(_ string, _ func(data []byte)) error {
	return nil
}

// Close is a no-op.
func (b *NoopBroadcaster) Close() error {
	return nil
}
