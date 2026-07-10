# Team contract

`spec` makes ownership, review, and stage changes visible, but it does not
replace clear handoffs. These conventions keep the Git-backed workflow calm.

## Start from the shared view

Open `spec` before picking up work. The Dashboard shows your queue; Pipeline
shows the team. If you are resuming after time offline, refresh (`r`) and read
the latest discussion before editing.

## One driver, explicit handoffs

A spec should have a clear driver. Claim it with `g c` in the TUI or
`spec assign`. Hand work over explicitly rather than changing author,
assignment, or stage by convention alone.

## Own the section, not the whole document

Section owner markers define which role should author each part. Different
roles can safely move different sections forward, but simultaneous edits to the
same section still require coordination.

Use reader threads for questions and review feedback instead of rewriting
another role's section. Anchor a question to the exact block with `A` when the
context matters.

## Resolve review work deliberately

The reader defaults to open threads. Step through them with `n` / `p`, reply or
resolve from the thread pane, and finish the review pass summary before
advancing. Do not resolve a thread merely to satisfy a gate; record the outcome
in the spec or decision log.

## Let gates communicate readiness

Use `a` to advance and let the configured gates explain what is missing. Do not
bypass gates by manually changing frontmatter. Use `v` to send work back with a
reason and `x` to block it when progress genuinely cannot continue.

## Publish or intentionally keep local

The team's `sync.auto_push` policy determines whether edits and comments publish
automatically. When it is `off`, use TUI `p` or `spec push` before handing work
over. `queued-offline` means local work is waiting for connectivity; do not edit
the managed clone by hand.

## Treat conflicts as coordination prompts

If `spec` names a conflicting spec and section, stop and coordinate with the
other editor. Re-running the command does not resolve the disagreement.

Never use force as a routine shortcut. Forced inbound sync can overwrite local
work and should be used only when the team has agreed which version wins.

## Keep transitions flowing

Advance work as it becomes ready rather than batching many stage changes at
standup. Small, continuous transitions reduce merge contention and make the
Dashboard trustworthy.

> Short version: open `spec`, claim work, own your sections, review threads to
> completion, hand over explicitly, and publish before stepping away.
