package email

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"

	"github.com/go-chi/chi/v5"

	"laika/pkg/logger"
)

// -- DTOs (HTTP boundary only) ------------------------------------------------

type sendRequest struct {
	Emails      []string `json:"emails"`
	MessageType string   `json:"message_type"`
	Subject     string   `json:"subject"`
	HTMLBody    string   `json:"html_body"`
}

type recipientResult struct {
	Email     string `json:"email"`
	Success   bool   `json:"success"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

type sendResponse struct {
	Success   bool              `json:"success"`
	RequestID string            `json:"request_id"`
	Results   []recipientResult `json:"results"`
	Error     string            `json:"error,omitempty"`
}

// -- Handler ------------------------------------------------------------------

// Handler handles POST /noti/email/{flow}. It resolves the named email flow
// from the URL, validates the request, delegates to the matching Service, and
// maps the result to the appropriate HTTP status.
type Handler struct {
	registry *Registry
	log      *slog.Logger
}

func NewHandler(registry *Registry, log *slog.Logger) *Handler {
	return &Handler{registry: registry, log: log}
}

func (h *Handler) SendEmail(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context(), h.log)

	flow := chi.URLParam(r, "flow")
	svc, ok := h.registry.Get(flow)
	if !ok {
		log.Warn("unknown email flow", "flow", flow, "available", h.registry.Names())
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": fmt.Sprintf("unknown email flow %q", flow),
		})
		return
	}

	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warn("invalid request body", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if msg := validate(req); msg != "" {
		log.Warn("request validation failed", "reason", msg)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}

	log.Info("email send request",
		"flow", flow,
		"recipient_count", len(req.Emails),
		"message_type", req.MessageType,
	)

	svcResp := svc.Send(r.Context(), Request{
		Emails:      req.Emails,
		MessageType: req.MessageType,
		Subject:     req.Subject,
		HTMLBody:    req.HTMLBody,
	})

	results := make([]recipientResult, len(svcResp.Results))
	for i, res := range svcResp.Results {
		results[i] = recipientResult{
			Email:     res.Email,
			Success:   res.Success,
			MessageID: res.MessageID,
			Error:     res.Error,
		}
	}

	status := http.StatusOK
	if !svcResp.Success {
		status = http.StatusInternalServerError
	}

	log.Info("email send outcome",
		"request_id", svcResp.RequestID,
		"success", svcResp.Success,
	)

	writeJSON(w, status, sendResponse{
		Success:   svcResp.Success,
		RequestID: svcResp.RequestID,
		Results:   results,
		Error:     svcResp.Error,
	})
}

// -- Validation ---------------------------------------------------------------

func validate(req sendRequest) string {
	if len(req.Emails) == 0 {
		return "emails must not be empty"
	}
	for _, addr := range req.Emails {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Sprintf("invalid email address: %s", addr)
		}
	}
	if req.MessageType == "" {
		return "message_type must not be empty"
	}
	if req.Subject == "" {
		return "subject must not be empty"
	}
	if req.HTMLBody == "" {
		return "html_body must not be empty"
	}
	return ""
}

// -- Helpers ------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
