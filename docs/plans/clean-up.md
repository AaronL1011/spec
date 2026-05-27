### Specifically what can go:

 canceledThroughGen — dead concept. We don't have real
 cancellation. Glamour can't be interrupted mid-render. This
 field exists to filter stale results from a fake cancel that
 never actually stops anything. With a renderer that completes
 fast, stale results simply don't matter — the gen check alone
 is enough.

 activeRenderGen + activeRenderKey — overcomplicated. renderGen
  alone is sufficient. We only need: "does this result match
 what I'm currently waiting for?"

 readerState enum (idle/pending/ready/failed) — mostly
 redundant now. The only state that drives real behaviour in
 viewReader() is readerFailed and whether readerContent == "".
 readerPending and readerIdle are never checked in the view
 path — openedReader does that job instead. The enum is noise.

 openedReader flag — exists purely because readerContent == ""
 needed to mean two different things: "first open, be blank" vs
 "genuinely no content". That ambiguity itself is a symptom of
 over-engineered state. With fast renders, readerContent is
 populated almost immediately, making openedReader barely
 observable in practice.

 context.Canceled error handling in sectionRenderedMsg — the
 context timeout is 20 seconds. Nothing cancels it in practice
 since we removed real cancellation. This branch is dead code.

 renderMetrics struct — never displayed anywhere, never
 surfaced to the user. Accumulating invisible counters.

 ### What's genuinely correct and should stay:

 - renderInFlight + pendingRequest — single-flight + coalesce
   pattern, right.
 - renderGen for stale result filtering — necessary.
 - readerCache — definitely keep.
 - viewport.Model for scrolling — correct Bubble Tea primitive.
 - cancelRender() clearing in-flight state on exit — correct.
 - Frame contract (normalizeContentLines) — correct and needed.
 - Spinner in status bar driven by renderInFlight — correct.

 ──────────────────────────────────────────────────────────────

 What alignment work should be done

 1. Collapse readerState enum to a single readerErr check —
    only failure state matters in the view. Remove
    idle/pending/ready.
 2. Remove canceledThroughGen — replace the msg.Gen <=
    m.canceledThroughGen guard with just msg.Gen != m.renderGen
     (only care about current).
 3. Remove activeRenderGen and activeRenderKey — renderGen is
    already incremented per request; that's all we need.
 4. Remove dead context cancel error branch — renders don't get
    canceled.
 5. Remove renderMetrics — or promote it to a visible debug
    display if it should exist.
 6. Simplify openedReader — initialise readerContent to a
    sentinel (e.g. a single space) on first open so the
    empty-string check works without a separate flag.
