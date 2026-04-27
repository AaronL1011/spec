package effects

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
	syncengine "github.com/aaronl1011/spec/internal/sync"
)

// NotifierAdapter bridges adapter.CommsAdapter to the Notifier interface.
type NotifierAdapter struct {
	Comms  adapter.CommsAdapter
	SpecID string
	Title  string
}

func (n *NotifierAdapter) Notify(ctx context.Context, target, message string) error {
	return n.Comms.Notify(ctx, adapter.Notification{
		SpecID:  n.SpecID,
		Title:   n.Title,
		Message: message,
		Mention: target,
	})
}

// WebhookerAdapter implements the Webhooker interface using net/http.
type WebhookerAdapter struct {
	Client *http.Client
}

func (w *WebhookerAdapter) Call(ctx context.Context, url string, payload map[string]interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling webhook payload: %w", err)
	}

	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// LoggerAdapter bridges store.DB and markdown decision log to the Logger interface.
type LoggerAdapter struct {
	DB      *store.DB
	SpecDir string
	SpecID  string
}

func (l *LoggerAdapter) LogDecision(ctx context.Context, specID, decision string) error {
	if l.SpecDir == "" {
		return nil
	}
	path := l.SpecDir + "/" + specID + ".md"
	_, err := markdown.AppendDecision(path, decision, "system")
	return err
}

func (l *LoggerAdapter) LogEvent(ctx context.Context, specID, event string, data map[string]interface{}) error {
	if l.DB == nil {
		return nil
	}
	meta, _ := json.Marshal(data)
	return l.DB.ActivityLog(specID, event, event, string(meta), "system")
}

// SyncerAdapter bridges the sync engine to pipeline transition effects.
type SyncerAdapter struct {
	Docs             adapter.DocsAdapter
	DB               *store.DB
	SpecDir          string
	ConflictStrategy string
	OwnerRole        string
	UserName         string
	DryRun           bool
}

func (s *SyncerAdapter) Sync(ctx context.Context, direction, specID string) error {
	if s.Docs == nil || s.SpecDir == "" {
		return nil
	}
	engine := syncengine.NewEngine(s.Docs, s.DB)
	_, err := engine.Run(ctx, syncengine.Options{
		SpecID:           specID,
		SpecPath:         s.SpecDir + "/" + specID + ".md",
		Direction:        direction,
		ConflictStrategy: s.ConflictStrategy,
		OwnerRole:        s.OwnerRole,
		UserName:         s.UserName,
		DryRun:           s.DryRun,
	})
	return err
}

// PMUpdaterAdapter bridges adapter.PMAdapter to the PMUpdater interface.
type PMUpdaterAdapter struct {
	PM      adapter.PMAdapter
	EpicKey string
}

func (p *PMUpdaterAdapter) UpdateStatus(ctx context.Context, status string) error {
	if p.PM == nil || p.EpicKey == "" {
		return nil
	}
	return p.PM.UpdateStatus(ctx, p.EpicKey, status)
}
