package agent

import (
	"context"
	"testing"
)

func TestCacheRecordReplay(t *testing.T) {
	dir := t.TempDir()
	inner := &scripted{resps: []*Response{textResp("first")}}
	rec := &CachedChatter{Inner: inner, Dir: dir, Mode: CacheRecord}
	req := &Request{Model: "m", Messages: []Message{{Role: "user", Content: "q"}}}
	if _, err := rec.Chat(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	rep := &CachedChatter{Inner: &scripted{}, Dir: dir, Mode: CacheReplay}
	resp, err := rep.Chat(context.Background(), req)
	if err != nil || resp.Choices[0].Message.Content != "first" || resp.Usage.Cost != 0 {
		t.Fatalf("replay: %v %+v", err, resp)
	}
	other := &Request{Model: "m", Messages: []Message{{Role: "user", Content: "different"}}}
	if _, err := rep.Chat(context.Background(), other); err == nil {
		t.Fatal("a replay miss must fail, not call the model")
	}
}
