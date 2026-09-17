// server_scroll_test.go pins the §G wheel-scroll region: the page publishes
// the DRAWN SERVER LOG rect only while on screen, and ScrollRegion drives
// the same logScroll the j/k keys move, clamped at both ends
// (d>0 = toward the newest).
package pages

import (
	"fmt"
	"testing"
)

// serverLogState is §G running with n plain log lines (passed through
// verbatim by the compactor, so assertions can read them).
func serverLogState(n int) ServerState {
	st := serverRunningState()
	for i := 1; i <= n; i++ {
		st.Log = append(st.Log, fmt.Sprintf("log line %02d", i))
	}

	return st
}

// logPage builds §G with a 30-line log at 80×24 (narrow stacked-log layout).
func logPage(t *testing.T) *Server {
	t.Helper()
	s := serverPage(t, serverLogState(30), 80, 24)
	_ = s.View().Content // View records the drawn section Rects

	return s
}

func TestServerScrollRegionsPublishesLogRect(t *testing.T) {
	t.Parallel()

	s := logPage(t)

	regions := s.ScrollRegions()
	if len(regions) != 1 {
		t.Fatalf("§G with a log must publish exactly one scroll region, got %#v", regions)
	}
	r := regions[0]
	if r.ID != RegionServerLog {
		t.Errorf("region id = %q, want %q", r.ID, RegionServerLog)
	}
	if r.Rect.W <= 0 || r.Rect.H <= 0 {
		t.Errorf("published rect must be a drawn box, got %v", r.Rect)
	}
	// The rect is CONTENT-RELATIVE (the hit map adds frame.ContentOrigin).
	if r.Rect.Y < 1 {
		t.Errorf("content-relative log rect Y = %d, want below the header row", r.Rect.Y)
	}

	// No log lines: the LOG pane never renders, nothing is published.
	empty := serverPage(t, serverRunningState(), 80, 24)
	_ = empty.View().Content
	if got := empty.ScrollRegions(); len(got) != 0 {
		t.Errorf("§G without log lines published regions %#v, want none", got)
	}
}

func TestServerScrollRegionDrivesLogScroll(t *testing.T) {
	t.Parallel()

	s := logPage(t)

	// d<0 = viewport UP: the same direction 'k' moves logScroll.
	if !s.ScrollRegion(RegionServerLog, -3) {
		t.Fatal("the page must own its own region id")
	}
	if s.logScroll != 3 {
		t.Fatalf("after 3 lines up logScroll = %d, want 3", s.logScroll)
	}
	// d>0 = viewport DOWN: toward the newest, clamped at 0.
	s.ScrollRegion(RegionServerLog, 1)
	if s.logScroll != 2 {
		t.Fatalf("after one line down logScroll = %d, want 2", s.logScroll)
	}
	s.ScrollRegion(RegionServerLog, 99)
	if s.logScroll != 0 {
		t.Fatalf("wheel-down past the newest must clamp at 0, got %d", s.logScroll)
	}
	// UP clamps at the same ceiling the keyboard uses (len(log)-1).
	s.ScrollRegion(RegionServerLog, -999)
	if want := len(s.state.Log) - 1; s.logScroll != want {
		t.Fatalf("wheel-up past the oldest must clamp at %d, got %d", want, s.logScroll)
	}
	// Unknown region: owned by no one, changes nothing.
	if s.ScrollRegion("nope:region", 1) {
		t.Error("an unknown region id must report false")
	}
	if s.logScroll != len(s.state.Log)-1 {
		t.Fatalf("unknown region changed logScroll to %d", s.logScroll)
	}
}

func TestServerScrollRegionInertWhileDetailOpen(t *testing.T) {
	t.Parallel()

	s := logPage(t)
	s.routesFocused = true
	s.detailIdx = 0
	s.detailOpen = true
	_ = s.View().Content // the detail view replaces the whole body

	if got := s.ScrollRegions(); len(got) != 0 {
		t.Errorf("the detail view draws no LOG pane, regions = %#v", got)
	}
	if s.ScrollRegion(RegionServerLog, 1) {
		t.Error("ScrollRegion must be inert while the detail view owns the body")
	}
}
