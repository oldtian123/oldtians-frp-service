// Package heartbeat runs the periodic heartbeat loop and triggers a config sync
// whenever the server reports a newer config version.
package heartbeat

import (
	"context"
	"log"
	"time"

	"github.com/oldtian123/oldtians-frp-service/agent/internal/serverapi"
)

// InfoProvider returns the current heartbeat payload (config version, lan ip,
// agent version, frpc status). It is called once per beat.
type InfoProvider func() serverapi.HeartbeatRequest

// Run sends a heartbeat every interval. On need_sync it invokes onNeedSync.
// Network errors are logged and retried; the loop never exits on its own.
func Run(ctx context.Context, client *serverapi.Client, interval time.Duration, info InfoProvider, onNeedSync func(), logger *log.Logger) {
	if logger == nil {
		logger = log.Default()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	beat := func() {
		hbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		resp, err := client.Heartbeat(hbCtx, info())
		if err != nil {
			logger.Printf("heartbeat: failed: %v", err)
			return
		}
		if resp.NeedSync {
			logger.Printf("heartbeat: server reports config version %d, triggering sync", resp.LatestConfigVersion)
			onNeedSync()
		}
	}

	// Beat immediately so a freshly started agent registers presence quickly.
	beat()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			beat()
		}
	}
}
