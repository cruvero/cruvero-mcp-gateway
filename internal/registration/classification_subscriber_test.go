package registration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/policy"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestClassificationSubscriberInvalidatesCache(t *testing.T) {
	t.Parallel()

	cache := policy.NewClassificationCache(0)
	cache.Set("exec", &types.ToolClassification{
		ToolName:  "exec",
		RiskLevel: types.RiskDestructive,
	})

	if cached := cache.Get("exec"); cached == nil {
		t.Fatal("expected cache entry before event")
	}

	broadcaster := newChannelBroadcaster()
	sub := NewClassificationSubscriber(broadcaster, cache, testRegistrationLogger())
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}

	evt := NewClassificationEvent("exec", "safe", "admin")
	data, _ := json.Marshal(evt)
	if err := broadcaster.Publish(SubjectClassificationUpdated, data); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if cached := cache.Get("exec"); cached != nil {
		t.Fatalf("expected cache entry to be invalidated, got %+v", cached)
	}
}

func TestClassificationSubscriberMalformedJSON(t *testing.T) {
	t.Parallel()

	cache := policy.NewClassificationCache(0)
	cache.Set("exec", &types.ToolClassification{
		ToolName:  "exec",
		RiskLevel: types.RiskDestructive,
	})

	broadcaster := newChannelBroadcaster()
	sub := NewClassificationSubscriber(broadcaster, cache, testRegistrationLogger())
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}

	// Malformed JSON should not panic or invalidate anything.
	if err := broadcaster.Publish(SubjectClassificationUpdated, []byte("bad json")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if cached := cache.Get("exec"); cached == nil {
		t.Fatal("expected cache entry to survive malformed event")
	}
}

func TestClassificationSubscriberEmptyToolName(t *testing.T) {
	t.Parallel()

	cache := policy.NewClassificationCache(0)
	cache.Set("exec", &types.ToolClassification{
		ToolName:  "exec",
		RiskLevel: types.RiskDestructive,
	})

	broadcaster := newChannelBroadcaster()
	sub := NewClassificationSubscriber(broadcaster, cache, testRegistrationLogger())
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}

	evt := ClassificationEvent{ToolName: " ", RiskLevel: "safe"}
	data, _ := json.Marshal(evt)
	if err := broadcaster.Publish(SubjectClassificationUpdated, data); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if cached := cache.Get("exec"); cached == nil {
		t.Fatal("expected cache entry to survive empty tool name event")
	}
}

func TestClassificationSubscriberWildcardInvalidatesAll(t *testing.T) {
	t.Parallel()

	cache := policy.NewClassificationCache(0)
	cache.Set("exec", &types.ToolClassification{
		ToolName:  "exec",
		RiskLevel: types.RiskDestructive,
	})
	cache.Set("read_file", &types.ToolClassification{
		ToolName:  "read_file",
		RiskLevel: types.RiskReadOnly,
	})

	broadcaster := newChannelBroadcaster()
	sub := NewClassificationSubscriber(broadcaster, cache, testRegistrationLogger())
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}

	evt := NewClassificationEvent("*", "pruned", "admin")
	data, _ := json.Marshal(evt)
	if err := broadcaster.Publish(SubjectClassificationUpdated, data); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if cached := cache.Get("exec"); cached != nil {
		t.Fatal("expected exec cache entry to be invalidated by wildcard")
	}
	if cached := cache.Get("read_file"); cached != nil {
		t.Fatal("expected read_file cache entry to be invalidated by wildcard")
	}
}

func TestClassificationSubscriberNilCache(t *testing.T) {
	t.Parallel()

	broadcaster := newChannelBroadcaster()
	sub := NewClassificationSubscriber(broadcaster, nil, testRegistrationLogger())
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}

	evt := NewClassificationEvent("exec", "safe", "admin")
	data, _ := json.Marshal(evt)

	// Should not panic with nil cache.
	if err := broadcaster.Publish(SubjectClassificationUpdated, data); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func TestClassificationSubscriberNilBroadcaster(t *testing.T) {
	t.Parallel()

	sub := NewClassificationSubscriber(nil, nil, nil)
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start with nil broadcaster should not error: %v", err)
	}
}

func TestClassificationSubscriberNilReceiver(t *testing.T) {
	t.Parallel()

	var sub *ClassificationSubscriber
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start nil subscriber should not error: %v", err)
	}
}
