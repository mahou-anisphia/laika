package email

// Registry holds named email flows. Each flow is an independent Service with
// its own SMTP provider. To register a new flow, add it to the map passed to
// NewRegistry — the handler resolves flows by name from the URL path.
type Registry struct {
	flows map[string]*Service
}

func NewRegistry(flows map[string]*Service) *Registry {
	return &Registry{flows: flows}
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
