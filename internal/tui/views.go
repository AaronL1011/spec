package tui

import (
	tea "charm.land/bubbletea/v2"
)

func (a *App) switchView(v View) tea.Cmd {
	// Close detail if switching views.
	a.markDetailRefreshDone()
	if a.showDetail {
		a.detail.cancelRender()
		a.stopWatch()
	}
	a.showDetail = false
	a.closeTriageDetailPane()
	a.activeView = v
	a.tabs.SetActive(int(v))
	a.statusBar.SetView(v.Label())
	a.statusBar.SetActiveRefreshKey(refreshKeyForView(v))
	a.syncBusyState()
	// The dashboard makes a network call (PR reviews), so re-fetching every time
	// the user switches back to it is spammy. On switch, fetch only if it has
	// never loaded (e.g. the startup fetch failed); otherwise leave updates to
	// the auto-timer tick and manual refresh, both of which still refresh it
	// unconditionally via refreshActiveView.
	if v == ViewDashboard && a.dashboard.loaded {
		return nil
	}
	return a.initAndRefreshView(v)
}

// initAndRefreshView initialises a view if it hasn't been loaded, or refreshes it.
func (a *App) initAndRefreshView(v View) tea.Cmd {
	switch v {
	case ViewDashboard:
		return a.scheduleRefresh(refreshKeyDashboard, a.dashboard.refresh())
	case ViewPipeline:
		if a.pipeline.loading {
			return a.scheduleRefresh(refreshKeyPipeline, a.pipeline.init())
		}
		return a.scheduleRefresh(refreshKeyPipeline, a.pipeline.refresh())
	case ViewSpecs:
		if a.specs.loading {
			return a.scheduleRefresh(refreshKeySpecs, a.specs.init())
		}
		return a.scheduleRefresh(refreshKeySpecs, a.specs.refresh())
	case ViewTriage:
		if a.triage.loading {
			return a.scheduleRefresh(refreshKeyTriage, a.triage.init())
		}
		return a.scheduleRefresh(refreshKeyTriage, a.triage.refresh())
	case ViewReviews:
		if a.reviews.loading {
			return a.scheduleRefresh(refreshKeyReviews, a.reviews.init())
		}
		return a.scheduleRefresh(refreshKeyReviews, a.reviews.refresh())
	default:
		return nil
	}
}

func (a *App) refreshActiveView() tea.Cmd {
	if a.showDetail {
		if a.detail.readerMode {
			return nil
		}
		return a.scheduleRefresh(a.detailRefreshKey(), a.detail.fetchData())
	}
	return a.initAndRefreshView(a.activeView)
}

func (a *App) delegateToActive(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch a.activeView {
	case ViewDashboard:
		a.dashboard, cmd = a.dashboard.update(msg)
	case ViewPipeline:
		a.pipeline, cmd = a.pipeline.update(msg)
	case ViewSpecs:
		a.specs, cmd = a.specs.update(msg)
	case ViewTriage:
		a.triage, cmd = a.triage.update(msg)
	case ViewReviews:
		a.reviews, cmd = a.reviews.update(msg)
	case ViewSettings:
		a.settings, cmd = a.settings.update(msg)
	}
	return cmd
}

func (a *App) propagateSize() {
	a.header.SetWidth(a.width)
	a.tabs.SetWidth(a.width)
	a.statusBar.SetWidth(a.width)

	ch := a.contentHeight()
	a.dashboard.setSize(a.width, ch)
	a.pipeline.setSize(a.width, ch)
	a.specs.setSize(a.width, ch)
	a.triage.setSize(a.width, ch)
	if a.showTriageDetail && a.triageDetail != nil {
		a.triageDetail.setSize(a.width, ch)
	}
	a.reviews.setSize(a.width, ch)
	a.settings.setSize(a.width, ch)
	if a.showDetail {
		a.detail.setSize(a.width, ch)
	}
	a.modal.SetSize(a.width, ch)
	a.help.setSize(a.width, a.height)
	a.standup.setSize(a.width, ch)
	if a.triageEdit.active {
		a.triageEdit.setSize(a.width, ch)
	}
	if a.revert.active {
		a.revert.setWidth(a.width)
	}
}

func (a App) contentHeight() int {
	return a.layout().contentHeight
}
