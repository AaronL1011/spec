# spec — Team Contract

spec's goal is to keep a team's direction in sync, visible & moving in the right direction; without having to manage excessive process coordination, minimising manual notification of cross-functional collaborators, ensuring the user interacts with as few tools as possible. It trys to be largely automatic and out of the way, but it can't fix bad coordination. The habits below are what keep it working; break them and we create conflicts and rework for ourselves.

1. **One spec, one driver; hand over explicitly.** Each spec has an author/owner who drives it through the pipeline. Don't advance, edit, or restructure someone else's spec without an explicit handover. Stepping in uninvited guarantees conflicts and lost effort; if you need to take over, say so and agree on it first.

2. **Own your section.** Edit only the sections marked for your role. Disjoint edits to a shared spec will merge cleanly, but two people in the _same_ section is guaranteed to conflict.

3. **Finish or discard before you step away.** No half-edited specs left overnight.

4. **Stagger advances.** Advance specs as you finish them through the day, not all at standup. Synchronized bursts are the only thing that manufactures collisions.

5. **Sync before resuming after time offline.** Run `spec status` once before picking work back up. It drains your queue deliberately.

6. **Never use `SPEC_FORCE`.** It _discards_ work. It is not a "make it go" button. Recovery is automatic.

7. **A conflict message is a conversation, not a glitch.** When spec names a spec and section, stop and coordinate with whoever else touched it. Re-running won't fix it.

8. **Trust the queue.** `queued-offline` is normal and safe. Don't panic-push or hand-edit the clone, it drains on your next command.

> **In one line:** Drive your own specs, own your sections, hand over explicitly, finish your edits, stagger your advances, and never force.
