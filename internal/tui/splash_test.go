package tui

import "testing"

// TestSplash_ReadyBeforeMinFrames_DelaysFade verifies the splash honours its
// minimum exposure time: data landing early must not cut the opening
// animation short — the fade only begins once splashMinFrames have elapsed.
func TestSplash_ReadyBeforeMinFrames_DelaysFade(t *testing.T) {
	s := newSplash()
	s.readyForExit()
	for range splashMinFrames - 1 {
		s.nextFrame()
	}
	if s.fading {
		t.Fatalf("fade started at frame %d, before minimum exposure of %d frames", s.frame, splashMinFrames)
	}
	s.nextFrame()
	if !s.fading {
		t.Fatalf("fade did not start at frame %d (minimum exposure %d)", s.frame, splashMinFrames)
	}
}

// TestSplash_ReadyAfterMinFrames_FadesOnNextTick verifies the floor never
// delays a slow fetch: once the animation has run its minimum exposure, the
// fade starts on the first tick after the payload lands.
func TestSplash_ReadyAfterMinFrames_FadesOnNextTick(t *testing.T) {
	s := newSplash()
	for range splashMinFrames + 5 {
		s.nextFrame()
	}
	s.readyForExit()
	s.nextFrame()
	if !s.fading {
		t.Fatal("fade did not start on the first tick after data landed past minimum exposure")
	}
}

// TestSplash_NotReady_NeverFades verifies the splash holds indefinitely while
// the first payload is still in flight.
func TestSplash_NotReady_NeverFades(t *testing.T) {
	s := newSplash()
	for range splashMinFrames * 2 {
		s.nextFrame()
	}
	if s.fading {
		t.Fatal("splash faded without data readiness")
	}
	if s.done() {
		t.Fatal("splash reported done without data readiness")
	}
}

// TestSplash_Done_AfterFullFade verifies done() only reports once the
// fade-out has fully landed, counting from the fade start at the minimum
// exposure frame.
func TestSplash_Done_AfterFullFade(t *testing.T) {
	s := newSplash()
	s.readyForExit()
	for range splashMinFrames {
		s.nextFrame()
	}
	if s.done() {
		t.Fatal("done reported before the fade-out completed")
	}
	for range splashFadeFrames {
		s.nextFrame()
	}
	if !s.done() {
		t.Fatal("done not reported after the fade-out completed")
	}
}
