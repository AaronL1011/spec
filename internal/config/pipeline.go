// Package config handles loading and resolution of team and user configuration.
package config

import "strings"

// PipelineConfig defines the configurable pipeline stages.
type PipelineConfig struct {
	// Preset is the name of a built-in pipeline preset (e.g., "minimal", "product").
	// If set, the preset's stages are used as the base configuration.
	Preset string `yaml:"preset,omitempty"`

	// Skip lists stage names to remove from the preset.
	// Only meaningful when Preset is set.
	Skip []string `yaml:"skip,omitempty"`

	// Stages defines the pipeline stages. When Preset is set, these override
	// or extend the preset's stages. When Preset is empty, these are the
	// complete stage definitions.
	Stages []StageConfig `yaml:"stages,omitempty"`

	// Variants defines alternative pipelines for different work types.
	Variants map[string]VariantConfig `yaml:"variants,omitempty"`

	// Default is the default variant name when multiple variants exist.
	Default string `yaml:"default,omitempty"`

	// VariantFromLabels maps spec labels to variant names for auto-selection.
	VariantFromLabels []LabelVariantMapping `yaml:"variant_from_labels,omitempty"`
}

// Owners represents one or more owner roles for a pipeline stage.
// In YAML, it can be specified as a single string or an array:
//
//	owner: pm           # single owner
//	owner: [pm, tl]     # multiple owners
type Owners []string

// UnmarshalYAML allows owner to be a string or array in YAML.
func (o *Owners) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try single string first
	var single string
	if err := unmarshal(&single); err == nil {
		if single != "" {
			*o = []string{single}
		}
		return nil
	}
	// Try array
	var list []string
	if err := unmarshal(&list); err != nil {
		return err
	}
	*o = list
	return nil
}

// Contains returns true if role is one of the owners.
func (o Owners) Contains(role string) bool {
	for _, r := range o {
		if r == role {
			return true
		}
	}
	return false
}

// String returns owners as a comma-separated string for display.
func (o Owners) String() string {
	return strings.Join(o, ", ")
}

// IsEmpty returns true if no owners are defined.
func (o Owners) IsEmpty() bool {
	return len(o) == 0
}

// StageConfig defines a single pipeline stage.
type StageConfig struct {
	// Name is the stage identifier (lowercase, underscores allowed).
	Name string `yaml:"name"`

	// Owner is the role(s) that own this stage. Can be a single role
	// or an array of roles in YAML.
	Owner Owners `yaml:"owner,omitempty"`

	// OwnerRole is the legacy field for backward compatibility.
	// Deprecated: Use Owner instead.
	OwnerRole string `yaml:"owner_role,omitempty"`

	// Icon is an emoji displayed in pipeline views.
	Icon string `yaml:"icon,omitempty"`

	// Optional marks the stage as skippable in the pipeline flow.
	Optional bool `yaml:"optional,omitempty"`

	// SkipWhen is an expression that, when true, causes the stage to be
	// automatically skipped during advancement.
	SkipWhen string `yaml:"skip_when,omitempty"`

	// Gates are conditions that must be satisfied to advance from this stage.
	Gates []GateConfig `yaml:"gates,omitempty"`

	// Warnings are time-based alerts that don't block advancement.
	Warnings []WarningConfig `yaml:"warnings,omitempty"`

	// Transitions customizes advance and revert behavior.
	Transitions TransitionsConfig `yaml:"transitions,omitempty"`

	// OnEnter lists effects to execute when entering this stage.
	OnEnter []EffectConfig `yaml:"on_enter,omitempty"`

	// OnExit lists effects to execute when leaving this stage.
	OnExit []EffectConfig `yaml:"on_exit,omitempty"`

	// AutoArchive moves the spec to archive/ when entering this stage.
	AutoArchive bool `yaml:"auto_archive,omitempty"`

	// Review configures plan review requirements for this stage.
	// Used primarily for the engineering stage to require technical plan approval.
	Review *StageReviewConfig `yaml:"review,omitempty"`

	// AutoAdvance configures automatic stage advancement when gates are satisfied.
	AutoAdvance *AutoAdvanceConfig `yaml:"auto_advance,omitempty"`
}

// StageReviewConfig configures plan review requirements.
type StageReviewConfig struct {
	// Required indicates whether review is required to advance.
	// Defaults to true when Review is present.
	Required *bool `yaml:"required,omitempty"`

	// Reviewers lists who can review (roles like "tl" or named users like "@mike").
	// Special value "author" allows self-review.
	Reviewers []string `yaml:"reviewers,omitempty"`

	// MinApprovals is the minimum number of approvals required.
	// Defaults to 1.
	MinApprovals int `yaml:"min_approvals,omitempty"`
}

// IsRequired returns whether review is required.
// Defaults to true if not explicitly set.
func (r *StageReviewConfig) IsRequired() bool {
	if r == nil {
		return false
	}
	if r.Required == nil {
		return true
	}
	return *r.Required
}

// GetMinApprovals returns the minimum approvals or default of 1.
func (r *StageReviewConfig) GetMinApprovals() int {
	if r == nil || r.MinApprovals == 0 {
		return 1
	}
	return r.MinApprovals
}

// AutoAdvanceConfig configures automatic stage advancement.
type AutoAdvanceConfig struct {
	// Enabled allows explicitly disabling auto-advance even if When is set.
	Enabled *bool `yaml:"enabled,omitempty"`

	// When is an expression that triggers auto-advance when true.
	// Example: "prs.all_approved and prs.threads_resolved"
	When string `yaml:"when,omitempty"`

	// Notify lists targets to notify on auto-advance.
	// Example: ["author", "next_owner"]
	Notify []string `yaml:"notify,omitempty"`

	// QuietHours prevents auto-advance during specified hours.
	// Format: "HH:MM-HH:MM" in local timezone.
	// Example: "22:00-08:00"
	QuietHours string `yaml:"quiet_hours,omitempty"`

	// RequireApproval requires a specific role to have approved (e.g., "tl").
	RequireApproval string `yaml:"require_approval,omitempty"`

	// ExcludeLabels prevents auto-advance for specs with these labels.
	ExcludeLabels []string `yaml:"exclude_labels,omitempty"`
}

// IsEnabled returns whether auto-advance is enabled.
// Defaults to true if When is set and Enabled is not explicitly false.
func (a *AutoAdvanceConfig) IsEnabled() bool {
	if a == nil || a.When == "" {
		return false
	}
	if a.Enabled == nil {
		return true
	}
	return *a.Enabled
}

// GetOwner returns the effective owner as a display string.
// For multiple owners, returns comma-separated list.
// Falls back to legacy OwnerRole if Owner is not set.
func (s StageConfig) GetOwner() string {
	if !s.Owner.IsEmpty() {
		return s.Owner.String()
	}
	return s.OwnerRole
}

// HasOwner returns true if role is an owner of this stage.
// Also returns true if no owners are defined (open stage) or if role is empty.
func (s StageConfig) HasOwner(role string) bool {
	if role == "" {
		return true
	}
	// Check new Owner field first
	if !s.Owner.IsEmpty() {
		return s.Owner.Contains(role)
	}
	// Fall back to legacy OwnerRole
	if s.OwnerRole != "" {
		return s.OwnerRole == role
	}
	// No owner defined - anyone can act
	return true
}

// GateConfig defines a gate condition for stage advancement.
type GateConfig struct {
	// Simple gate types (mutually exclusive with Expr)
	SectionNotEmpty string          `yaml:"section_not_empty,omitempty"`
	SectionComplete string          `yaml:"section_complete,omitempty"` // Deprecated: use SectionNotEmpty
	PRStackExists   *bool           `yaml:"pr_stack_exists,omitempty"`  // Deprecated: use StepsExists
	StepsExists     *bool           `yaml:"steps_exists,omitempty"`     // Build plan has at least one step
	PRsApproved     *bool           `yaml:"prs_approved,omitempty"`
	ReviewApproved  *bool           `yaml:"review_approved,omitempty"` // Technical plan review is approved
	Duration        string          `yaml:"duration,omitempty"`
	LinkExists      *LinkExistsGate `yaml:"link_exists,omitempty"`

	// Expression gate
	Expr    string `yaml:"expr,omitempty"`
	Message string `yaml:"message,omitempty"`

	// Logical operators (contain nested gates)
	All []GateConfig `yaml:"all,omitempty"`
	Any []GateConfig `yaml:"any,omitempty"`
	Not *GateConfig  `yaml:"not,omitempty"`
}

// GetSectionNotEmpty returns the section slug for section_not_empty or section_complete gates.
func (g GateConfig) GetSectionNotEmpty() string {
	if g.SectionNotEmpty != "" {
		return g.SectionNotEmpty
	}
	return g.SectionComplete
}

// Type returns the gate type as a string for display.
func (g GateConfig) Type() string {
	switch {
	case g.SectionNotEmpty != "":
		return "section_not_empty"
	case g.SectionComplete != "":
		return "section_complete" // backward compat
	case g.StepsExists != nil:
		return "steps_exists"
	case g.PRStackExists != nil:
		return "steps_exists" // backward compat, map to new name
	case g.PRsApproved != nil:
		return "prs_approved"
	case g.ReviewApproved != nil:
		return "review_approved"
	case g.Duration != "":
		return "duration"
	case g.LinkExists != nil:
		return "link_exists"
	case g.Expr != "":
		return "expr"
	case len(g.All) > 0:
		return "all"
	case len(g.Any) > 0:
		return "any"
	case g.Not != nil:
		return "not"
	default:
		return "unknown"
	}
}

// HasStepsExists returns true if either StepsExists or legacy PRStackExists is set.
func (g GateConfig) HasStepsExists() bool {
	return (g.StepsExists != nil && *g.StepsExists) || (g.PRStackExists != nil && *g.PRStackExists)
}

// HasReviewApproved returns true if ReviewApproved gate is set.
func (g GateConfig) HasReviewApproved() bool {
	return g.ReviewApproved != nil && *g.ReviewApproved
}

// Value returns the gate's primary value as a string for display.
func (g GateConfig) Value() string {
	switch {
	case g.SectionNotEmpty != "":
		return g.SectionNotEmpty
	case g.SectionComplete != "":
		return g.SectionComplete
	case g.PRStackExists != nil:
		return "true"
	case g.PRsApproved != nil:
		return "true"
	case g.Duration != "":
		return g.Duration
	case g.Expr != "":
		return g.Expr
	default:
		return ""
	}
}

// IsSimple returns true if this is a simple (non-logical) gate.
func (g GateConfig) IsSimple() bool {
	return len(g.All) == 0 && len(g.Any) == 0 && g.Not == nil
}

// LinkExistsGate checks for a link in a specific section.
type LinkExistsGate struct {
	Section string `yaml:"section"`
	Type    string `yaml:"type,omitempty"` // e.g., "figma", "github"
}

// WarningConfig defines a time-based warning for a stage.
type WarningConfig struct {
	// After is the duration after which the warning triggers (e.g., "5d", "48h").
	After string `yaml:"after"`

	// Message is displayed when the warning is active.
	Message string `yaml:"message"`

	// Notify is the target to notify (e.g., "tl", "#channel").
	Notify string `yaml:"notify,omitempty"`
}

// TransitionsConfig customizes stage transition behavior.
type TransitionsConfig struct {
	Advance TransitionConfig `yaml:"advance,omitempty"`
	Revert  TransitionConfig `yaml:"revert,omitempty"`
}

// TransitionConfig defines behavior for a specific transition type.
type TransitionConfig struct {
	// To lists allowed target stages. Empty means default (next for advance,
	// any previous for revert).
	To []string `yaml:"to,omitempty"`

	// Gates are additional conditions for this specific transition.
	Gates []GateConfig `yaml:"gates,omitempty"`

	// Require lists required fields (e.g., ["reason"] for reverts).
	Require []string `yaml:"require,omitempty"`

	// Effects are side effects to execute on transition.
	Effects []EffectConfig `yaml:"effects,omitempty"`
}

// EffectConfig defines a side effect for stage transitions.
type EffectConfig struct {
	// Notify sends a notification. Can be a string (target) or NotifyEffect.
	Notify *NotifyEffect `yaml:"notify,omitempty"`

	// Sync triggers document sync ("outbound" or "inbound").
	Sync string `yaml:"sync,omitempty"`

	// UpdatePM updates the PM tool (Jira, Linear, etc.).
	UpdatePM *UpdatePMEffect `yaml:"update_pm,omitempty"`

	// LogDecision adds an entry to the decision log.
	LogDecision string `yaml:"log_decision,omitempty"`

	// Increment increments a frontmatter counter (e.g., "revert_count").
	Increment string `yaml:"increment,omitempty"`

	// Webhook calls an external URL.
	Webhook *WebhookEffect `yaml:"webhook,omitempty"`

	// Archive moves the spec to archive/.
	Archive bool `yaml:"archive,omitempty"`

	// Trigger invokes a named workflow or action.
	Trigger string `yaml:"trigger,omitempty"`

	// When is an expression that must be true for this effect to run.
	When string `yaml:"when,omitempty"`
}

// NotifyEffect configures a notification.
type NotifyEffect struct {
	// Target is who to notify (e.g., "next_owner", "tl", "#channel", "@user").
	Target string `yaml:"target,omitempty"`

	// Targets allows multiple notification targets.
	Targets []string `yaml:"targets,omitempty"`

	// Channel overrides the default channel.
	Channel string `yaml:"channel,omitempty"`

	// Template is the notification template name.
	Template string `yaml:"template,omitempty"`
}

// UnmarshalYAML allows notify to be a string or object.
func (n *NotifyEffect) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try string first
	var target string
	if err := unmarshal(&target); err == nil {
		n.Target = target
		return nil
	}

	// Try string array
	var targets []string
	if err := unmarshal(&targets); err == nil {
		n.Targets = targets
		return nil
	}

	// Try full object
	type plain NotifyEffect
	return unmarshal((*plain)(n))
}

// UpdatePMEffect configures PM tool updates.
type UpdatePMEffect struct {
	Status string `yaml:"status,omitempty"`
}

// WebhookEffect configures an external webhook call.
type WebhookEffect struct {
	URL     string            `yaml:"url"`
	Method  string            `yaml:"method,omitempty"` // default: POST
	Headers map[string]string `yaml:"headers,omitempty"`
	Body    map[string]string `yaml:"body,omitempty"`
	Timeout string            `yaml:"timeout,omitempty"` // default: 10s
}

// VariantConfig defines a pipeline variant for different work types.
type VariantConfig struct {
	// Preset is the base preset for this variant.
	Preset string `yaml:"preset,omitempty"`

	// Skip lists stages to remove from the preset.
	Skip []string `yaml:"skip,omitempty"`

	// Stages defines or overrides stages for this variant.
	Stages []StageConfig `yaml:"stages,omitempty"`
}

// LabelVariantMapping maps a spec label to a pipeline variant.
type LabelVariantMapping struct {
	// Label is the spec label to match.
	Label string `yaml:"label,omitempty"`

	// Variant is the variant name to use when the label matches.
	Variant string `yaml:"variant"`

	// Default marks this as the default variant when no labels match.
	Default bool `yaml:"default,omitempty"`
}

// StageByName returns the stage config with the given name, or nil.
func (p PipelineConfig) StageByName(name string) *StageConfig {
	for i := range p.Stages {
		if p.Stages[i].Name == name {
			return &p.Stages[i]
		}
	}
	return nil
}

// StageIndex returns the index of the stage with the given name, or -1.
func (p PipelineConfig) StageIndex(name string) int {
	for i, s := range p.Stages {
		if s.Name == name {
			return i
		}
	}
	return -1
}

// StageNames returns the names of all stages in order.
func (p PipelineConfig) StageNames() []string {
	names := make([]string, len(p.Stages))
	for i, s := range p.Stages {
		names[i] = s.Name
	}
	return names
}

// IsValidTransition returns true if transitioning from `from` to `to` is valid
// (i.e., `to` comes after `from` in the pipeline).
func (p PipelineConfig) IsValidTransition(from, to string) bool {
	fromIdx := p.StageIndex(from)
	toIdx := p.StageIndex(to)
	return fromIdx >= 0 && toIdx > fromIdx
}

// IsValidReversion returns true if reverting from `from` to `to` is valid
// (i.e., `to` comes before `from` in the pipeline).
func (p PipelineConfig) IsValidReversion(from, to string) bool {
	fromIdx := p.StageIndex(from)
	toIdx := p.StageIndex(to)
	return fromIdx >= 0 && toIdx >= 0 && toIdx < fromIdx
}

// NewSimpleGate creates a simple gate config for common gate types.
func NewSimpleGate(gateType, value string) GateConfig {
	t := true
	switch gateType {
	case "section_not_empty":
		return GateConfig{SectionNotEmpty: value}
	case "steps_exists", "pr_stack_exists":
		return GateConfig{StepsExists: &t}
	case "prs_approved":
		return GateConfig{PRsApproved: &t}
	case "review_approved":
		return GateConfig{ReviewApproved: &t}
	case "duration":
		return GateConfig{Duration: value}
	case "expr":
		return GateConfig{Expr: value}
	default:
		return GateConfig{}
	}
}

// NewExprGate creates an expression gate with a custom message.
func NewExprGate(expr, message string) GateConfig {
	return GateConfig{Expr: expr, Message: message}
}
