# Configurable Pipelines

Spec pipelines define how work flows through your team. Every spec moves through stages — from intake to done — with gates that ensure quality and effects that automate handoffs.

## Quick Start

### 1. Choose a preset

When initializing your team config, select a preset that matches your workflow:

```bash
spec config init
# Interactive: select from presets
# Or specify directly:
spec config init --preset startup
```

### 2. View your pipeline

```bash
spec pipeline              # Compact view
spec pipeline --verbose    # Show gates and effects
spec pipeline presets      # List all available presets
```

### 3. Customize as needed

```bash
spec pipeline add security_review --after build --owner security
spec pipeline remove design
spec pipeline validate     # Check for errors
```

---

## Presets

Presets are curated pipelines for common team structures. Start with one and customize from there.

| Preset | Best For | Stages |
|--------|----------|--------|
| `minimal` | Solo / tiny teams | triage → draft → build → review → done |
| `startup` | Fast-moving product teams | triage → draft → review → build → pr_review → done |
| `product` | Full product teams with design & QA | triage → draft → review → design → engineering → build → pr_review → qa → done |
| `platform` | Infrastructure teams | triage → draft → review → rfc → build → pr_review → security → done → rollout → monitoring |
| `kanban` | Continuous flow teams | backlog → ready → in_progress → review → done |

View preset details:

```bash
spec pipeline presets
```

---

## Configuration

Pipeline configuration lives in `spec.config.yaml`:

### Using a preset

```yaml
pipeline:
  preset: startup
```

### Using a preset with modifications

```yaml
pipeline:
  preset: startup
  skip: [design]           # Remove stages you don't need
  stages:                   # Add or modify stages
    - name: security_review
      owner: security
      gates:
        - section_not_empty: security_considerations
```

### Defining stages from scratch

```yaml
pipeline:
  stages:
    - name: triage
      owner: pm
      icon: 📥
    
    - name: draft
      owner: pm
      icon: 📝
    
    - name: review
      owner: tl
      icon: 👀
      gates:
        - section_not_empty: problem_statement
        - section_not_empty: goals_non_goals
    
    - name: build
      owner: engineer
      icon: 🏗️
      gates:
        - section_not_empty: acceptance_criteria
    
    - name: done
      owner: tl
      icon: 🎉
```

---

## Stages

Each stage has:

| Field | Description | Example |
|-------|-------------|---------|
| `name` | Stage identifier (lowercase, underscores) | `pr_review` |
| `owner` | Role that owns this stage | `engineer`, `pm`, `tl`, `qa`, `designer`, `security`, `anyone`, `author` |
| `icon` | Emoji shown in views | `🏗️` |
| `optional` | Can be skipped in normal flow | `true` |
| `gates` | Conditions to exit this stage | See [Gates](#gates) |
| `skip_when` | Expression to auto-skip this stage | See [Conditional Stages](#conditional-stages) |

### Adding a stage

```bash
# Interactive
spec pipeline add

# With flags
spec pipeline add security_review --after build --owner security --icon 🔒
```

### Removing a stage

```bash
spec pipeline remove design
spec pipeline remove design --force  # Skip confirmation
```

---

## Gates

Gates are conditions that must be true before advancing from a stage. If any gate fails, `spec advance` shows what needs to be fixed.

### Built-in gates

```yaml
gates:
  # Section must have content
  - section_not_empty: problem_statement
  
  # PR stack plan must exist in §7.3
  - pr_stack_exists: true
  
  # All PRs must be approved
  - prs_approved: true
```

### Expression gates

For complex conditions, use expressions:

```yaml
gates:
  # All decisions must be resolved
  - expr: "decisions.unresolved == 0"
    message: "Resolve all open decisions before advancing"
  
  # At least 3 acceptance criteria
  - expr: "acceptance_criteria.items.count >= 3"
    message: "Add at least 3 acceptance criteria"
  
  # Spec has been in stage for 24 hours (cooling period)
  - expr: "spec.time_in_stage >= duration('24h')"
    message: "Wait 24 hours before advancing"
```

### Expression context

Expressions have access to:

| Variable | Description |
|----------|-------------|
| `spec.id` | Spec ID (e.g., "SPEC-042") |
| `spec.title` | Spec title |
| `spec.status` | Current stage |
| `spec.labels` | Labels array |
| `spec.word_count` | Total word count |
| `spec.time_in_stage` | Duration in current stage |
| `spec.revert_count` | Number of times reverted |
| `decisions.total` | Total decisions |
| `decisions.resolved` | Resolved decisions |
| `decisions.unresolved` | Open decisions |
| `acceptance_criteria.items.count` | Number of AC items |
| `acceptance_criteria.checked` | Checked AC items |
| `pr_stack.exists` | Has PR stack plan |
| `pr_stack.total` | Total PRs planned |
| `prs.total` | Actual PRs created |
| `prs.approved` | Approved PRs |
| `prs.merged` | Merged PRs |

### Checking gates

```bash
spec validate SPEC-042        # Check if spec can advance
spec advance SPEC-042 --dry-run  # Preview what would happen
```

---

## Conditional Stages

Use `skip_when` to automatically skip stages based on the spec:

```yaml
stages:
  - name: design
    owner: designer
    skip_when: "'no-design' in spec.labels"
  
  - name: qa
    owner: qa
    skip_when: "'skip-qa' in spec.labels || spec.word_count < 100"
  
  - name: security_review
    owner: security
    skip_when: "'internal' in spec.labels"
```

When a spec has the matching label, that stage is skipped in the pipeline flow.

---

## Effects

Effects are actions that run automatically on stage transitions.

```yaml
stages:
  - name: review
    owner: tl
    transitions:
      advance:
        effects:
          - notify: "@pm-team"
          - sync: outbound
      revert:
        effects:
          - notify: "$author"
          - log_decision: "Sent back by $user: requires rework"
```

### Available effects

| Effect | Description |
|--------|-------------|
| `notify: <target>` | Send notification to user/channel |
| `sync: outbound` | Sync spec to external docs |
| `sync: inbound` | Pull updates from external docs |
| `log_decision: "<message>"` | Add entry to decision log |
| `archive: true` | Archive the spec |
| `webhook: { url: "..." }` | Call external webhook |
| `trigger: <pipeline>` | Trigger CI/CD pipeline |

### Template variables in effects

Messages support variable expansion:

- `$spec_id` — Spec ID
- `$spec_title` — Spec title
- `$from_stage` — Previous stage
- `$to_stage` — New stage
- `$user` — User who triggered the transition
- `$author` — Spec author

---

## Variants

For teams with different workflows for different work types, use variants:

```yaml
pipeline:
  default: standard
  
  variant_from_labels:
    - label: bug
      variant: bugfix
    - label: hotfix
      variant: hotfix
    - default: true
      variant: standard
  
  variants:
    standard:
      preset: product
    
    bugfix:
      preset: startup
      skip: [design]
    
    hotfix:
      stages:
        - name: triage
          owner: tl
        - name: build
          owner: engineer
        - name: done
          owner: tl
```

When a spec has the `bug` label, it uses the `bugfix` variant (startup preset minus design). Hotfixes get an even shorter pipeline.

---

## Commands Reference

| Command | Description |
|---------|-------------|
| `spec pipeline` | Show current pipeline |
| `spec pipeline --verbose` | Show gates and effects |
| `spec pipeline presets` | List available presets |
| `spec pipeline export` | Export resolved config as YAML |
| `spec pipeline add [name]` | Add a new stage |
| `spec pipeline remove <name>` | Remove a stage |
| `spec pipeline edit <name>` | Edit a stage |
| `spec pipeline validate` | Validate configuration |

---

## Examples

### Minimal solo setup

```yaml
pipeline:
  preset: minimal
```

### Startup with security review

```yaml
pipeline:
  preset: startup
  stages:
    - name: security_review
      owner: security
      gates:
        - expr: "'security' in spec.labels"
          message: "Security review required for security-labeled specs"
```

### Enterprise with full gates

```yaml
pipeline:
  preset: product
  stages:
    - name: review
      owner: tl
      gates:
        - section_not_empty: problem_statement
        - section_not_empty: goals_non_goals
        - expr: "decisions.unresolved == 0"
          message: "All decisions must be resolved"
      transitions:
        advance:
          effects:
            - notify: "#product-specs"
            - sync: outbound
    
    - name: build
      owner: engineer
      gates:
        - section_not_empty: acceptance_criteria
        - expr: "acceptance_criteria.items.count >= 3"
          message: "Need at least 3 acceptance criteria"
        - pr_stack_exists: true
```

### Conditional QA based on risk

```yaml
pipeline:
  preset: product
  stages:
    - name: qa
      owner: qa
      skip_when: "'low-risk' in spec.labels && spec.word_count < 500"
      gates:
        - prs_approved: true
```

---

## Validation

Always validate after making changes:

```bash
spec pipeline validate
```

This checks:
- All stages have valid owners
- Gate expressions compile correctly
- Skip references exist
- No duplicate stage names

---

## Migration from v0.x

If you have an existing `pipeline.stages` config, it continues to work. The new features are additive:

| Old | New equivalent |
|-----|----------------|
| `owner_role: pm` | `owner: pm` |
| `section_complete: x` | `section_not_empty: x` |

Both forms are supported for backward compatibility.
