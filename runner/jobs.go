package runner

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"plugin"
	"strconv"
	"strings"

	"github.com/gosom/google-maps-scraper/deduper"
	"github.com/gosom/google-maps-scraper/exiter"
	"github.com/gosom/google-maps-scraper/gmaps"
	"github.com/gosom/scrapemate"
)

func CreateSeedJobs(
	fastmode bool,
	langCode string,
	r io.Reader,
	maxDepth int,
	email bool,
	geoCoordinates string,
	zoom int,
	radius float64,
	dedup deduper.Deduper,
	exitMonitor exiter.Exiter,
	extraReviews bool,
	searchDelay int,
) (jobs []scrapemate.IJob, err error) {
	var lat, lon float64

	if fastmode {
		// Parse global geo coordinates if provided (optional — per-query #!geo# can override)
		if geoCoordinates != "" && geoCoordinates != "0,0" {
			parts := strings.Split(geoCoordinates, ",")
			if len(parts) == 2 {
				if qlat, parseErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64); parseErr == nil {
					if qlon, parseErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); parseErr == nil {
						if qlat >= -90 && qlat <= 90 && qlon >= -180 && qlon <= 180 {
							lat = qlat
							lon = qlon
						}
					}
				}
			}
		}
		// lat/lon may remain 0,0 if global geo is empty — per-query #!geo# coordinates
		// will be parsed in the loop below. Queries without any geo will be skipped.

		if zoom < 1 || zoom > 21 {
			zoom = 15
		}

		if radius < 0 {
			radius = 10000
		}
	}

	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		query := strings.TrimSpace(scanner.Text())
		if query == "" {
			continue
		}

		var id string

		if before, after, ok := strings.Cut(query, "#!#"); ok {
			query = strings.TrimSpace(before)
			id = strings.TrimSpace(after)
		}

		// Parse per-query geo coordinates (format: query#!geo#lat,lon)
		queryLat, queryLon := lat, lon
		queryGeo := geoCoordinates
		hasPerQueryGeo := false

		if before, after, ok := strings.Cut(query, "#!geo#"); ok {
			query = strings.TrimSpace(before)
			geoParts := strings.Split(strings.TrimSpace(after), ",")
			if len(geoParts) == 2 {
				if qlat, err := strconv.ParseFloat(geoParts[0], 64); err == nil {
					if qlon, err := strconv.ParseFloat(geoParts[1], 64); err == nil {
						queryLat = qlat
						queryLon = qlon
						queryGeo = geoParts[0] + "," + geoParts[1]
						hasPerQueryGeo = true
					}
				}
			}
		}

		// For FastMode: verify per-query coords are valid if global coords were not set
		if fastmode && !hasPerQueryGeo && (lat == 0 && lon == 0) {
			continue // skip queries without valid coordinates in fast mode
		}

		var job scrapemate.IJob

		if !fastmode {
			opts := []gmaps.GmapJobOptions{}

			if dedup != nil {
				opts = append(opts, gmaps.WithDeduper(dedup))
			}

			if exitMonitor != nil {
				opts = append(opts, gmaps.WithExitMonitor(exitMonitor))
			}

			if extraReviews {
				opts = append(opts, gmaps.WithExtraReviews())
			}

			if searchDelay > 0 {
				opts = append(opts, gmaps.WithSearchDelay(searchDelay))
			}

			// Use per-query geo if available, otherwise global
			job = gmaps.NewGmapJob(id, langCode, query, maxDepth, email, queryGeo, zoom, opts...)
		} else {
			jparams := gmaps.MapSearchParams{
				Location: gmaps.MapLocation{
					Lat:     queryLat,
					Lon:     queryLon,
					ZoomLvl: float64(zoom),
					Radius:  radius,
				},
				Query:     query,
				ViewportW: 1920,
				ViewportH: 450,
				Hl:        langCode,
			}

			opts := []gmaps.SearchJobOptions{}

			if dedup != nil {
				opts = append(opts, gmaps.WithSearchJobDeduper(dedup))
			}

			if exitMonitor != nil {
				opts = append(opts, gmaps.WithSearchJobExitMonitor(exitMonitor))
			}

			if searchDelay > 0 {
				opts = append(opts, gmaps.WithSearchJobDelay(searchDelay))
			}

			// Use depth as max pages for pagination (1 = no pagination, 2+ = paginate)
			if maxDepth > 1 {
				opts = append(opts, gmaps.WithSearchJobMaxPages(maxDepth))
			}

			job = gmaps.NewSearchJob(&jparams, opts...)
		}

		jobs = append(jobs, job)
	}

	return jobs, scanner.Err()
}

func LoadCustomWriter(pluginDir, pluginName string) (scrapemate.ResultWriter, error) {
	files, err := os.ReadDir(pluginDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if filepath.Ext(file.Name()) != ".so" && filepath.Ext(file.Name()) != ".dll" {
			continue
		}

		pluginPath := filepath.Join(pluginDir, file.Name())

		p, err := plugin.Open(pluginPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open plugin %s: %w", file.Name(), err)
		}

		symWriter, err := p.Lookup(pluginName)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup symbol %s: %w", pluginName, err)
		}

		writer, ok := symWriter.(*scrapemate.ResultWriter)
		if !ok {
			return nil, fmt.Errorf("unexpected type %T from writer symbol in plugin %s", symWriter, file.Name())
		}

		return *writer, nil
	}

	return nil, fmt.Errorf("no plugin found in %s", pluginDir)
}
