package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Cache modes (EVAL_CACHE).
const (
	CacheOff    = "off"
	CacheRecord = "record"
	CacheReplay = "replay"
)

// CachedChatter records model responses keyed by sha256(model, messages,
// tools, params) under Dir, and replays them. Replay hits whenever a run sends
// exactly the same request again (same environment, same tool outputs), so
// graders and reports can be iterated without spending.
type CachedChatter struct {
	Inner Chatter
	Dir   string
	Mode  string
}

func (c *CachedChatter) key(req *Request) (string, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Chat serves from the cache or the inner client.
func (c *CachedChatter) Chat(ctx context.Context, req *Request) (*Response, error) {
	if c.Mode == "" || c.Mode == CacheOff {
		return c.Inner.Chat(ctx, req)
	}
	k, err := c.key(req)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(c.Dir, k[:2], k+".json")
	if raw, err := os.ReadFile(path); err == nil {
		var resp Response
		if err := json.Unmarshal(raw, &resp); err == nil {
			// A replayed response cost nothing this time.
			resp.Usage.Cost = 0
			return &resp, nil
		}
	}
	if c.Mode == CacheReplay {
		return nil, fmt.Errorf("model cache miss in replay mode (key %s)", k[:12])
	}
	resp, err := c.Inner.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	if raw, err := json.Marshal(resp); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, raw, 0o644)
	}
	return resp, nil
}
