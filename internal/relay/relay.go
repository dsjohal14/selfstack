package relay

// Relay handles AI processing and automation
type Relay struct {
	enabled bool
}

// New creates a new Relay instance
func New() *Relay {
	return &Relay{
		enabled: true,
	}
}

// IsEnabled returns whether the relay is enabled
func (r *Relay) IsEnabled() bool {
	return r.enabled
}

// Enable turns on the relay
func (r *Relay) Enable() {
	r.enabled = true
}

// Disable turns off the relay
func (r *Relay) Disable() {
	r.enabled = false
}

