// Package effects executes pipeline transition effects.
package effects

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nexl/spec-cli/internal/config"
)

// Result represents the outcome of executing an effect.
type Result struct {
	Effect    config.EffectConfig
	Success   bool
	Message   string
	Error     error
	Duration  time.Duration
	Skipped   bool
	SkipReason string
}

// TransitionType indicates whether we're advancing or reverting.
type TransitionType string

const (
	TransitionAdvance TransitionType = "advance"
	TransitionRevert  TransitionType = "revert"
)

// ExecutionContext holds context for effect execution.
type ExecutionContext struct {
	SpecID         string
	SpecTitle      string
	FromStage      string
	ToStage        string
	TransitionType TransitionType
	User           string
	UserRole       string
	DryRun         bool
	
	// Adapters for effects that need external services
	Notifier   Notifier
	Syncer     Syncer
	Webhooker  Webhooker
	Logger     Logger
}

// Notifier sends notifications.
type Notifier interface {
	Notify(ctx context.Context, target string, message string) error
}

// Syncer syncs to external systems.
type Syncer interface {
	Sync(ctx context.Context, direction string, specID string) error
}

// Webhooker calls webhooks.
type Webhooker interface {
	Call(ctx context.Context, url string, payload map[string]interface{}) error
}

// Logger logs decisions and events.
type Logger interface {
	LogDecision(ctx context.Context, specID string, decision string) error
	LogEvent(ctx context.Context, specID string, event string, data map[string]interface{}) error
}

// Executor runs effects for pipeline transitions.
type Executor struct {
	dryRun bool
}

// NewExecutor creates a new effect executor.
func NewExecutor(dryRun bool) *Executor {
	return &Executor{dryRun: dryRun}
}

// Execute runs all effects for a transition.
func (e *Executor) Execute(ctx context.Context, effects []config.EffectConfig, execCtx ExecutionContext) []Result {
	var results []Result

	for _, effect := range effects {
		start := time.Now()
		result := e.executeOne(ctx, effect, execCtx)
		result.Duration = time.Since(start)
		results = append(results, result)
	}

	return results
}

func (e *Executor) executeOne(ctx context.Context, effect config.EffectConfig, execCtx ExecutionContext) Result {
	// Dry run mode - just report what would happen
	if e.dryRun || execCtx.DryRun {
		return Result{
			Effect:     effect,
			Success:    true,
			Skipped:    true,
			SkipReason: "dry-run mode",
			Message:    fmt.Sprintf("would execute: %s", describeEffect(effect)),
		}
	}

	switch {
	case effect.Notify != nil:
		return e.executeNotify(ctx, effect, execCtx)
	case effect.Sync != "":
		return e.executeSync(ctx, effect, execCtx)
	case effect.LogDecision != "":
		return e.executeLogDecision(ctx, effect, execCtx)
	case effect.Increment != "":
		return e.executeIncrement(ctx, effect, execCtx)
	case effect.Archive:
		return e.executeArchive(ctx, effect, execCtx)
	case effect.Webhook != nil:
		return e.executeWebhook(ctx, effect, execCtx)
	case effect.Trigger != "":
		return e.executeTrigger(ctx, effect, execCtx)
	default:
		return Result{
			Effect:  effect,
			Success: false,
			Error:   fmt.Errorf("unknown effect type"),
		}
	}
}

func (e *Executor) executeNotify(ctx context.Context, effect config.EffectConfig, execCtx ExecutionContext) Result {
	if execCtx.Notifier == nil {
		return Result{
			Effect:     effect,
			Success:    true,
			Skipped:    true,
			SkipReason: "no notifier configured",
		}
	}

	// Build notification message
	message := buildNotificationMessage(effect, execCtx)

	// Get targets
	var targets []string
	if effect.Notify.Target != "" {
		targets = append(targets, effect.Notify.Target)
	}
	targets = append(targets, effect.Notify.Targets...)

	// Expand special targets
	expandedTargets := expandTargets(targets, execCtx)

	// Send notifications
	var errors []string
	for _, target := range expandedTargets {
		if err := execCtx.Notifier.Notify(ctx, target, message); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", target, err))
		}
	}

	if len(errors) > 0 {
		return Result{
			Effect:  effect,
			Success: false,
			Error:   fmt.Errorf("notification errors: %s", strings.Join(errors, "; ")),
		}
	}

	return Result{
		Effect:  effect,
		Success: true,
		Message: fmt.Sprintf("notified %s", strings.Join(expandedTargets, ", ")),
	}
}

func (e *Executor) executeSync(ctx context.Context, effect config.EffectConfig, execCtx ExecutionContext) Result {
	if execCtx.Syncer == nil {
		return Result{
			Effect:     effect,
			Success:    true,
			Skipped:    true,
			SkipReason: "no syncer configured",
		}
	}

	if err := execCtx.Syncer.Sync(ctx, effect.Sync, execCtx.SpecID); err != nil {
		return Result{
			Effect:  effect,
			Success: false,
			Error:   fmt.Errorf("sync failed: %w", err),
		}
	}

	return Result{
		Effect:  effect,
		Success: true,
		Message: fmt.Sprintf("synced %s", effect.Sync),
	}
}

func (e *Executor) executeLogDecision(ctx context.Context, effect config.EffectConfig, execCtx ExecutionContext) Result {
	if execCtx.Logger == nil {
		return Result{
			Effect:     effect,
			Success:    true,
			Skipped:    true,
			SkipReason: "no logger configured",
		}
	}

	decision := expandTemplate(effect.LogDecision, execCtx)
	if err := execCtx.Logger.LogDecision(ctx, execCtx.SpecID, decision); err != nil {
		return Result{
			Effect:  effect,
			Success: false,
			Error:   fmt.Errorf("log decision failed: %w", err),
		}
	}

	return Result{
		Effect:  effect,
		Success: true,
		Message: fmt.Sprintf("logged decision: %s", decision),
	}
}

func (e *Executor) executeIncrement(ctx context.Context, effect config.EffectConfig, execCtx ExecutionContext) Result {
	// Increment is handled by the store layer
	// Here we just return success - the actual increment happens elsewhere
	return Result{
		Effect:  effect,
		Success: true,
		Message: fmt.Sprintf("increment %s", effect.Increment),
	}
}

func (e *Executor) executeArchive(ctx context.Context, effect config.EffectConfig, execCtx ExecutionContext) Result {
	// Archive is handled by the advance command
	// Here we signal that archiving should happen
	return Result{
		Effect:  effect,
		Success: true,
		Message: "archive spec",
	}
}

func (e *Executor) executeWebhook(ctx context.Context, effect config.EffectConfig, execCtx ExecutionContext) Result {
	if execCtx.Webhooker == nil {
		return Result{
			Effect:     effect,
			Success:    true,
			Skipped:    true,
			SkipReason: "no webhooker configured",
		}
	}

	payload := map[string]interface{}{
		"spec_id":    execCtx.SpecID,
		"spec_title": execCtx.SpecTitle,
		"from_stage": execCtx.FromStage,
		"to_stage":   execCtx.ToStage,
		"transition": string(execCtx.TransitionType),
		"user":       execCtx.User,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}

	// Merge custom body fields
	for k, v := range effect.Webhook.Body {
		payload[k] = v
	}

	if err := execCtx.Webhooker.Call(ctx, effect.Webhook.URL, payload); err != nil {
		return Result{
			Effect:  effect,
			Success: false,
			Error:   fmt.Errorf("webhook failed: %w", err),
		}
	}

	return Result{
		Effect:  effect,
		Success: true,
		Message: fmt.Sprintf("called webhook %s", effect.Webhook.URL),
	}
}

func (e *Executor) executeTrigger(ctx context.Context, effect config.EffectConfig, execCtx ExecutionContext) Result {
	// Trigger is for CI/CD pipelines - implementation depends on integration
	return Result{
		Effect:  effect,
		Success: true,
		Message: fmt.Sprintf("trigger %s", effect.Trigger),
	}
}

// Helper functions

func describeEffect(e config.EffectConfig) string {
	switch {
	case e.Notify != nil:
		if e.Notify.Target != "" {
			return fmt.Sprintf("notify %s", e.Notify.Target)
		}
		if len(e.Notify.Targets) > 0 {
			return fmt.Sprintf("notify %s", strings.Join(e.Notify.Targets, ", "))
		}
		return "notify"
	case e.Sync != "":
		return fmt.Sprintf("sync %s", e.Sync)
	case e.LogDecision != "":
		return fmt.Sprintf("log decision: %s", e.LogDecision)
	case e.Increment != "":
		return fmt.Sprintf("increment %s", e.Increment)
	case e.Archive:
		return "archive"
	case e.Webhook != nil:
		return fmt.Sprintf("webhook %s", e.Webhook.URL)
	case e.Trigger != "":
		return fmt.Sprintf("trigger %s", e.Trigger)
	default:
		return "unknown effect"
	}
}

func buildNotificationMessage(effect config.EffectConfig, execCtx ExecutionContext) string {
	if effect.Notify.Template != "" {
		return expandTemplate(effect.Notify.Template, execCtx)
	}

	// Default message
	action := "advanced"
	if execCtx.TransitionType == TransitionRevert {
		action = "reverted"
	}

	return fmt.Sprintf("%s %s %s from %s to %s",
		execCtx.User,
		action,
		execCtx.SpecID,
		execCtx.FromStage,
		execCtx.ToStage,
	)
}

func expandTargets(targets []string, execCtx ExecutionContext) []string {
	var expanded []string
	for _, t := range targets {
		switch t {
		case "$author":
			expanded = append(expanded, execCtx.User)
		case "$owner":
			// Would need stage owner from context
			expanded = append(expanded, t)
		default:
			expanded = append(expanded, t)
		}
	}
	return expanded
}

func expandTemplate(template string, execCtx ExecutionContext) string {
	r := strings.NewReplacer(
		"$spec_id", execCtx.SpecID,
		"$spec_title", execCtx.SpecTitle,
		"$from_stage", execCtx.FromStage,
		"$to_stage", execCtx.ToStage,
		"$user", execCtx.User,
		"$role", execCtx.UserRole,
	)
	return r.Replace(template)
}

// ShouldArchive checks if any result indicates archiving should happen.
func ShouldArchive(results []Result) bool {
	for _, r := range results {
		if r.Effect.Archive && r.Success {
			return true
		}
	}
	return false
}

// HasErrors checks if any effect failed.
func HasErrors(results []Result) bool {
	for _, r := range results {
		if !r.Success && !r.Skipped {
			return true
		}
	}
	return false
}

// ErrorSummary returns a summary of all errors.
func ErrorSummary(results []Result) string {
	var errors []string
	for _, r := range results {
		if r.Error != nil {
			errors = append(errors, r.Error.Error())
		}
	}
	return strings.Join(errors, "; ")
}
