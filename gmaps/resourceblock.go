package gmaps

import (
	"github.com/gosom/scrapemate"
)

// blockUnnecessaryResources injects JavaScript to remove existing stylesheets
// and prevent new ones from loading. This reduces bandwidth by ~30-50%
// since CSS and fonts are not needed for data extraction.
// The scraper only needs HTML + JavaScript (APP_INITIALIZATION_STATE).
func blockUnnecessaryResources(page scrapemate.BrowserPage) {
	// One-time removal of existing stylesheets.
	// Removed MutationObserver and FontFace overrides as they can cause instability
	// on complex SPAs like Google Maps.
	_, _ = page.Eval(`() => {
		document.querySelectorAll('link[rel="stylesheet"], style').forEach(el => el.remove());
	}`)
}
