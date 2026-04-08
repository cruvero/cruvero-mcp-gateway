package proxy

import (
	"context"
	"hash/fnv"

	"github.com/cruvero/mcp-gateway/internal/types"
)

// SessionAffinityStrategy uses rendezvous (highest random weight) hashing to
// map a session ID to a consistent backend candidate. When no session ID is
// set, it delegates to a fallback strategy.
type SessionAffinityStrategy struct {
	fallback RoutingStrategy
}

// NewSessionAffinityStrategy creates a session affinity strategy that falls
// back to the given strategy when no session ID is available.
func NewSessionAffinityStrategy(fallback RoutingStrategy) *SessionAffinityStrategy {
	if fallback == nil {
		fallback = &RoundRobinStrategy{}
	}
	return &SessionAffinityStrategy{fallback: fallback}
}

// Select chooses a backend using rendezvous hashing when a session ID is set
// in the RoutingRequest, otherwise delegates to the fallback strategy.
func (s *SessionAffinityStrategy) Select(ctx context.Context, candidates []types.ServerRecord, req *RoutingRequest) (*types.ServerRecord, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if req == nil || req.SessionID == "" {
		return s.fallback.Select(ctx, candidates, req)
	}

	var best *types.ServerRecord
	var bestScore uint64
	for i := range candidates {
		score := hrwScore(req.SessionID, candidates[i].ID)
		if best == nil || score > bestScore {
			bestScore = score
			c := candidates[i]
			best = &c
		}
	}
	return best, nil
}

// hrwScore computes a rendezvous (HRW) hash score using FNV-1a 64-bit.
func hrwScore(sessionID, candidateID string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(sessionID))
	_, _ = h.Write([]byte(candidateID))
	return h.Sum64()
}
