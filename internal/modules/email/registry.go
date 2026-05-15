package email

// FlowStatus is the boot-time probe outcome for a single email flow. Reason is
// populated only when Available is false — either "not configured" or a real
// SMTP/auth error message.
type FlowStatus struct {
	Available bool
	Reason    string
}

// Registry holds named email flows. Each flow is an independent Service with
// its own SMTP provider. To register a new flow, add it to the map passed to
// NewRegistry — the handler resolves flows by name from the URL path.
//
// Flows have a status set at boot via SetStatus; the health endpoint reads
// these to report per-flow availability. A degraded flow is still callable —
// the dial just fails at request time, surfacing the same error to the caller.
type Registry struct {
	flows    map[string]*Service
	statuses map[string]FlowStatus
}

func NewRegistry(flows map[string]*Service) *Registry {
	return &Registry{
		flows:    flows,
		statuses: make(map[string]FlowStatus, len(flows)),
	}
}

// Get returns the Service for the given flow name, or false if not found.
func (r *Registry) Get(name string) (*Service, bool) {
	svc, ok := r.flows[name]
	return svc, ok
}

// Names returns all registered flow names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.flows))
	for name := range r.flows {
		names = append(names, name)
	}
	return names
}

// SetStatus records the boot-time probe result for a flow.
func (r *Registry) SetStatus(name string, s FlowStatus) {
	r.statuses[name] = s
}

// Statuses returns a copy-safe snapshot of every flow's boot-time status.
func (r *Registry) Statuses() map[string]FlowStatus {
	out := make(map[string]FlowStatus, len(r.statuses))
	for k, v := range r.statuses {
		out[k] = v
	}
	return out
}
