# spec — the daily workflow, from signal to a reviewed decision.
#
# Preflight:
#   make build
#
# The tape reseeds a complete, isolated HOME at /tmp/spec-vhs-demo. It never
# reads or writes the recorder's real ~/.spec state and needs no network.
# Render: vhs docs/demos/demo.tape

Output docs/demos/demo.gif

# ── Terminal settings ──────────────────────────────────────────────────────────
Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 720
Set Theme "Ayu Mirage"
Set TypingSpeed 55ms
Set WaitTimeout 10s
Env HOME "/tmp/spec-vhs-demo"

# ── Cold open ──────────────────────────────────────────────────────────────────
# Reseed and draw the card outside capture; the first visible frame is clean.
Hide
Type "./docs/demos/seed-demo.sh >/dev/null && ~/.spec/demo-banner.sh"
Enter
Sleep 1s
Show
Sleep 3s

# ── Daily workspace: Ada's personal queue ─────────────────────────────────────
# Launch outside the capture too, so the title card cuts cleanly to the TUI.
Type "spec"
Sleep 1s
Hide
Sleep 200ms
Ctrl+C
Type "~/.spec/demo-spec.sh"
Enter
Sleep 400ms
Show
Sleep 2s
Show

# Dashboard: clear personal work, one active build spec, one visible block.
Sleep 2000ms

# Team context is one key away: every active spec grouped by its stage.
Type "2"
Sleep 1800ms
Down
Sleep 350ms
Down
Sleep 900ms

# ── Return to Ada's selected work, then open the reader ───────────────────────
Type "1"
Sleep 1200ms
Down
Sleep 1100ms
Enter
Sleep 1600ms

# Overview establishes state, ownership, and section-level review counts.
Type "o"
Sleep 1800ms

# ── Review cockpit: one conversation, then one anchored follow-up ──────────────
# Jump directly to the architecture discussion and give the reader time to scan it.
Type "n"
Sleep 1800ms
Type "T"
Sleep 1500ms

# Add one concise answer. Wait for the textarea to exist before sending text.
Type "r"
Sleep 1s
Type "Tenant + policy."
Sleep 180ms
Type " An export burst shouldn't starve ordinary API calls."
Sleep 350ms
Ctrl+S
Sleep 1800ms

# Move once to QA's latency question and let the context settle.
Type "n"
Sleep 2200ms

# Focus the exact acceptance block and add one distinct follow-up question.
Tab
Sleep 500ms
Type "A"
Sleep 1200ms
Enter
Wait+Screen /ctrl.s send/
Sleep 700ms
Type "Should the emergency bucket"
Sleep 180ms
Type " have its own p99 threshold?"
Sleep 350ms
Ctrl+S
Sleep 1800ms

# ── Personalise: live theme preview, settle on Ayu Mirage ──────────────────────
Escape
Sleep 450ms
Escape
Sleep 600ms
Type "6"
Sleep 1400ms

# Name → role → handle → theme.
Down
Sleep 250ms
Down
Sleep 250ms
Down
Sleep 350ms
Enter
Sleep 500ms

# Preview a contrasting theme, then settle back on the demo's Ayu Mirage palette.
Set TypingSpeed 80ms
Type "llllllllllllllllllllll"
Sleep 900ms
Enter
Sleep 1200ms

Ctrl+C
