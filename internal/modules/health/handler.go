package health

import (
	"encoding/json"
	"net/http"
	"time"
)

// ComponentStatus is a boot-time snapshot for an optional module (email flow,
// Discord, etc.). Available=false with Reason set means the module is
// registered but unusable for this run.
type ComponentStatus struct {
	Available bool
	Reason    string
}

// Handler reports a static, boot-time view of which optional modules are usable.
// It does not re-probe on each request — checks at request time would add
// latency to liveness probes and could thrash unstable backends. If a module
// recovers mid-run, restart to refresh the snapshot.
//
// Status semantics:
//   - "ok":       every registered module is available
//   - "degraded": at least one module is available, at least one is not
//   - "down":     no module is available (this is when the response goes 503)
type Handler struct {
	Email   map[string]ComponentStatus
	Discord ComponentStatus
}

type componentDTO struct {
	Status string `json:"status"` // "ok" | "unavailable"
	Reason string `json:"reason,omitempty"`
}

type response struct {
	Status  string                  `json:"status"` // "ok" | "degraded" | "down"
	Email   map[string]componentDTO `json:"email"`
	Discord componentDTO            `json:"discord"`
	Time    string                  `json:"time"`
}

func (h *Handler) Check(w http.ResponseWriter, r *http.Request) {
	totalAvailable, totalRegistered := 0, 0

	emailOut := make(map[string]componentDTO, len(h.Email))
	for name, c := range h.Email {
		totalRegistered++
		if c.Available {
			totalAvailable++
			emailOut[name] = componentDTO{Status: "ok"}
		} else {
			emailOut[name] = componentDTO{Status: "unavailable", Reason: c.Reason}
		}
	}

	totalRegistered++
	var discordOut componentDTO
	if h.Discord.Available {
		totalAvailable++
		discordOut = componentDTO{Status: "ok"}
	} else {
		discordOut = componentDTO{Status: "unavailable", Reason: h.Discord.Reason}
	}

	overall := "ok"
	code := http.StatusOK
	switch {
	case totalAvailable == 0:
		overall = "down"
		code = http.StatusServiceUnavailable
	case totalAvailable < totalRegistered:
		overall = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(response{
		Status:  overall,
		Email:   emailOut,
		Discord: discordOut,
		Time:    time.Now().UTC().Format(time.RFC3339),
	})
}
