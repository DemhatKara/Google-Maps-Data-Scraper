package gmaps

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gosom/google-maps-scraper/deduper"
	"github.com/gosom/google-maps-scraper/exiter"
	"github.com/gosom/scrapemate"
)

const (
	resultsPerPage   = 20
	maxPaginationPages = 6 // max 6 pages = 120 results per query
)

type SearchJobOptions func(*SearchJob)

type MapLocation struct {
	Lat     float64
	Lon     float64
	ZoomLvl float64
	Radius  float64
}

type MapSearchParams struct {
	Location  MapLocation
	Query     string
	ViewportW int
	ViewportH int
	Hl        string
}

type SearchJob struct {
	scrapemate.Job

	params      *MapSearchParams
	ExitMonitor exiter.Exiter
	Deduper     deduper.Deduper
	SearchDelay int
	offset      int // pagination offset (0, 20, 40, ...)
	pageNum     int // current page number (0-based)
	maxPages    int // max pages to paginate (from depth setting)
}

func NewSearchJob(params *MapSearchParams, opts ...SearchJobOptions) *SearchJob {
	const (
		defaultPrio       = scrapemate.PriorityMedium
		defaultMaxRetries = 1
		baseURL           = "https://maps.google.com/search"
	)

	job := SearchJob{
		Job: scrapemate.Job{
			ID:         uuid.New().String(),
			Method:     http.MethodGet,
			URL:        baseURL,
			MaxRetries: defaultMaxRetries,
			Priority:   defaultPrio,
		},
		maxPages: maxPaginationPages,
	}

	job.params = params

	for _, opt := range opts {
		opt(&job)
	}

	// Build URL params with current offset
	job.Job.URLParams = buildGoogleMapsParams(params, job.offset)

	return &job
}

func WithSearchJobExitMonitor(exitMonitor exiter.Exiter) SearchJobOptions {
	return func(j *SearchJob) {
		j.ExitMonitor = exitMonitor
	}
}

func WithSearchJobDelay(d int) SearchJobOptions {
	return func(j *SearchJob) {
		j.SearchDelay = d
	}
}

func WithSearchJobDeduper(d deduper.Deduper) SearchJobOptions {
	return func(j *SearchJob) {
		j.Deduper = d
	}
}

func WithSearchJobMaxPages(n int) SearchJobOptions {
	return func(j *SearchJob) {
		if n > 0 && n <= maxPaginationPages {
			j.maxPages = n
		}
	}
}

func (j *SearchJob) Process(ctx context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	if j.SearchDelay > 0 {
		// add some randomness +- 30%
		randFactor := 0.7 + (0.6 * rand.Float64())
		sleepTime := time.Duration(float64(j.SearchDelay)*randFactor) * time.Second
		time.Sleep(sleepTime)
	}
	defer func() {
		resp.Document = nil
		resp.Body = nil
		resp.Meta = nil
	}()

	body := removeFirstLine(resp.Body)
	if len(body) == 0 {
		return nil, nil, fmt.Errorf("empty response body")
	}

	entries, err := ParseSearchResults(body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse search results: %w", err)
	}

	rawCount := len(entries) // count before filtering, for pagination decision

	entries = filterAndSortEntriesWithinRadius(entries,
		j.params.Location.Lat,
		j.params.Location.Lon,
		j.params.Location.Radius,
	)

	// Deduplicate entries by CID to avoid same place appearing in multiple district searches
	if j.Deduper != nil {
		unique := make([]*Entry, 0, len(entries))
		for _, e := range entries {
			// Use CID as unique key, fallback to title+address
			key := e.Cid
			if key == "" {
				key = e.Title + "|" + e.Address
			}
			if key != "" && j.Deduper.AddIfNotExists(ctx, key) {
				unique = append(unique, e)
			}
		}
		entries = unique
	}

	// Build next page jobs if we got a full page of results and haven't reached max depth
	var nextJobs []scrapemate.IJob
	nextPage := j.pageNum + 1
	if rawCount >= resultsPerPage && nextPage < j.maxPages {
		nextOffset := (nextPage) * resultsPerPage
		nextJob := &SearchJob{
			Job: scrapemate.Job{
				ID:         uuid.New().String(),
				Method:     http.MethodGet,
				URL:        "https://maps.google.com/search",
				URLParams:  buildGoogleMapsParams(j.params, nextOffset),
				MaxRetries: 1,
				Priority:   j.Job.Priority + 1, // slightly lower priority than seed jobs
			},
			params:      j.params,
			ExitMonitor: j.ExitMonitor,
			Deduper:     j.Deduper,
			SearchDelay: j.SearchDelay,
			offset:      nextOffset,
			pageNum:     nextPage,
			maxPages:    j.maxPages,
		}
		nextJobs = append(nextJobs, nextJob)

		// Track the pagination job in exit monitor
		if j.ExitMonitor != nil {
			j.ExitMonitor.IncrPlacesFound(1) // track the next-page job as a "place" to wait for
		}
	}

	if j.ExitMonitor != nil {
		if j.pageNum == 0 {
			// Only the first page counts as seed completion
			j.ExitMonitor.IncrSeedCompleted(1)
		} else {
			// Pagination pages complete their "place" tracking
			j.ExitMonitor.IncrPlacesCompleted(1)
		}
		j.ExitMonitor.IncrPlacesFound(len(entries))
		j.ExitMonitor.IncrPlacesCompleted(len(entries))
	}

	return entries, nextJobs, nil
}

func removeFirstLine(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	index := bytes.IndexByte(data, '\n')
	if index == -1 {
		return []byte{}
	}

	return data[index+1:]
}

func buildGoogleMapsParams(params *MapSearchParams, offset int) map[string]string {
	params.ViewportH = 800
	params.ViewportW = 600

	ans := map[string]string{
		"tbm":      "map",
		"authuser": "0",
		"hl":       params.Hl,
		"q":        params.Query,
	}

	pb := fmt.Sprintf("!4m12!1m3!1d3826.902183192154!2d%.4f!3d%.4f!2m3!1f0!2f0!3f0!3m2!1i%d!2i%d!4f%.1f!7i20!8i%d"+
		"!10b1!12m22!1m3!18b1!30b1!34e1!2m3!5m1!6e2!20e3!4b0!10b1!12b1!13b1!16b1!17m1!3e1!20m3!5e2!6b1!14b1!46m1!1b0"+
		"!96b1!19m4!2m3!1i360!2i120!4i8",
		params.Location.Lon,
		params.Location.Lat,
		params.ViewportW,
		params.ViewportH,
		params.Location.ZoomLvl,
		offset,
	)

	ans["pb"] = pb

	return ans
}
