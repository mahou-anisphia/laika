package email

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"laika/internal/provider"
	"laika/pkg/logger"
)

// Provider is the interface the service uses to send mail. Defined here so the
// service is testable without a real SMTP server.
type Provider interface {
	Send(recipients []string, msg provider.Message) ([]provider.Result, error)
}

// Request is the internal type passed to Service.Send. Not exposed over HTTP.
type Request struct {
	Emails      []string
	MessageType string
	Subject     string
	HTMLBody    string
}

// RecipientResult holds the outcome for a single recipient.
type RecipientResult struct {
	Email     string
	Success   bool
	MessageID string
	Error     string
}

// Response is the internal result returned by Service.Send.
type Response struct {
	Success   bool
	RequestID string
	Results   []RecipientResult
	Error     string
}

// Service orchestrates email delivery. It generates a request ID, calls the
// provider, and aggregates per-recipient results into a single Response.
type Service struct {
	provider Provider
	from     string
	log      *slog.Logger
}

func NewService(p Provider, from string, log *slog.Logger) *Service {
	return &Service{provider: p, from: from, log: log}
}

// Send delivers the request to all recipients and returns a consolidated result.
// It never returns an error — all failure modes are encoded in Response.
func (s *Service) Send(ctx context.Context, req Request) Response {
	requestID := uuid.New().String()
	log := logger.FromContext(ctx, s.log).With(
		"email_request_id", requestID,
		"message_type", req.MessageType,
		"recipient_count", len(req.Emails),
	)

	log.Info("email send started")

	providerResults, connErr := s.provider.Send(req.Emails, provider.Message{
		From:     s.from,
		Subject:  req.Subject,
		HTMLBody: req.HTMLBody,
	})

	// Connection-level failure: no sends were attempted.
	if connErr != nil {
		log.Error("smtp connection failed", "error", connErr)
		results := make([]RecipientResult, len(req.Emails))
		for i, addr := range req.Emails {
			results[i] = RecipientResult{Email: addr, Success: false, Error: connErr.Error()}
		}
		return Response{
			Success:   false,
			RequestID: requestID,
			Results:   results,
			Error:     fmt.Sprintf("smtp connection failed: %s", connErr.Error()),
		}
	}

	var successCount int
	results := make([]RecipientResult, len(providerResults))
	for i, r := range providerResults {
		res := RecipientResult{Email: req.Emails[i]}
		if r.Err != nil {
			res.Success = false
			res.Error = r.Err.Error()
			log.Warn("recipient failed", "email", req.Emails[i], "error", r.Err)
		} else {
			res.Success = true
			res.MessageID = r.MessageID
			successCount++
			log.Info("recipient succeeded", "email", req.Emails[i], "message_id", r.MessageID)
		}
		results[i] = res
	}

	total := len(req.Emails)
	resp := Response{
		Success:   successCount > 0,
		RequestID: requestID,
		Results:   results,
	}

	switch {
	case successCount == total:
		log.Info("email send complete", "success_count", successCount, "total", total)
	case successCount > 0:
		resp.Error = fmt.Sprintf("partial failure: %d of %d recipients failed", total-successCount, total)
		log.Warn("email send partial failure", "success_count", successCount, "total", total)
	default:
		resp.Error = fmt.Sprintf("all %d recipients failed", total)
		log.Error("email send failed", "total", total)
	}

	return resp
}
