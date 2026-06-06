# spec — Team Contract

`spec` keeps work visible by combining specs, ownership, stage changes, and
handoffs in one shared repo-backed workflow. It reduces coordination overhead, but
it still depends on clear ownership and disciplined edits. Use the rules below to
avoid conflicts and lost work.

1. **One spec, one driver; hand over explicitly.** Each spec has an author or owner
   who moves it through the pipeline. Do not advance, edit, or restructure someone
   else's spec without an explicit handover.

2. **Own your section.** Edit only the sections marked for your role. Different
   sections can merge cleanly; simultaneous edits to the same section can conflict.

3. **Finish or discard before you step away.** Do not leave half-edited specs
   sitting overnight.

4. **Stagger advances.** Advance specs as you finish them through the day, not all
   at standup. Large synchronized batches create avoidable collisions.

5. **Sync before resuming after time offline.** Run `spec status` before picking
   work back up so you see the latest stage, owner, and queue state.

6. **Do not use `SPEC_FORCE` as a shortcut.** It discards work. Use it only when you
   understand what will be overwritten and have coordinated with the other editor.

7. **Treat conflict messages as coordination prompts.** When `spec` names a spec
   and section, stop and coordinate with whoever else touched it. Re-running the
   same command will not resolve the conflict.

8. **Do not hand-edit the managed clone.** `queued-offline` means local work is
   waiting to be pushed. It drains on the next successful command.

> **Short version:** Drive your own specs, own your sections, hand over explicitly,
> finish edits before stepping away, stagger advances, and do not force unless you
> know what will be discarded.
