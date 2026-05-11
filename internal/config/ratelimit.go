package config

// RateLimit is the per-caller token bucket configuration. RPM is the steady-state
// rate (tokens added per minute) and Burst is the maximum bucket capacity.
type RateLimit struct {
	RPM   int
	Burst int
}
