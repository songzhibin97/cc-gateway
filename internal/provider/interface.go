package provider

import (
	"context"

	"github.com/songzhibin97/cc-gateway/internal/domain"
	"github.com/songzhibin97/cc-gateway/pkg/sse"
)

// Provider is the core interface every backend must implement.
type Provider interface {
	// Type returns the provider type.
	Type() domain.ProviderType

	// Stream sends the request to the upstream provider and writes
	// Anthropic-format SSE events to the writer.
	// Returns usage reported by the provider.
	Stream(ctx context.Context, account *domain.Account, req *domain.CanonicalRequest, w *sse.Writer) (*domain.Usage, error)
}
