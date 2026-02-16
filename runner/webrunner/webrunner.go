package webrunner

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosom/google-maps-scraper/common/logger"
	"github.com/gosom/google-maps-scraper/deduper"
	"github.com/gosom/google-maps-scraper/exiter"
	"github.com/gosom/google-maps-scraper/runner"
	"github.com/gosom/google-maps-scraper/tlmt"
	"github.com/gosom/google-maps-scraper/web"
	"github.com/gosom/google-maps-scraper/web/sqlite"
	"github.com/gosom/scrapemate"
	"github.com/gosom/scrapemate/scrapemateapp"
	"golang.org/x/sync/errgroup"
)

type webrunner struct {
	srv *web.Server
	svc *web.Service
	cfg *runner.Config
}

func New(cfg *runner.Config) (runner.Runner, error) {
	if cfg.DataFolder == "" {
		return nil, fmt.Errorf("data folder is required")
	}

	if err := os.MkdirAll(cfg.DataFolder, os.ModePerm); err != nil {
		return nil, err
	}

	const dbfname = "jobs.db"

	dbpath := filepath.Join(cfg.DataFolder, dbfname)

	repo, err := sqlite.New(dbpath)
	if err != nil {
		return nil, err
	}

	svc := web.NewService(repo, cfg.DataFolder)

	srv, err := web.New(svc, cfg.Addr)
	if err != nil {
		return nil, err
	}

	ans := webrunner{
		srv: srv,
		svc: svc,
		cfg: cfg,
	}

	return &ans, nil
}

func (w *webrunner) Run(ctx context.Context) error {
	egroup, ctx := errgroup.WithContext(ctx)

	egroup.Go(func() error {
		return w.work(ctx)
	})

	egroup.Go(func() error {
		return w.srv.Start(ctx)
	})

	return egroup.Wait()
}

func (w *webrunner) Close(context.Context) error {
	return nil
}

func (w *webrunner) work(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			jobs, err := w.svc.SelectPending(ctx)
			if err != nil {
				return err
			}

			for i := range jobs {
				select {
				case <-ctx.Done():
					return nil
				default:
					t0 := time.Now().UTC()
					if err := w.scrapeJob(ctx, &jobs[i]); err != nil {
						params := map[string]any{
							"job_count": len(jobs[i].Data.Keywords),
							"duration":  time.Now().UTC().Sub(t0).String(),
							"error":     err.Error(),
						}

						evt := tlmt.NewEvent("web_runner", params)

						_ = runner.Telemetry().Send(ctx, evt)

						logger.Error("error scraping job", "job_id", jobs[i].ID, "error", err)
					} else {
						params := map[string]any{
							"job_count": len(jobs[i].Data.Keywords),
							"duration":  time.Now().UTC().Sub(t0).String(),
						}

						_ = runner.Telemetry().Send(ctx, tlmt.NewEvent("web_runner", params))

						logger.Info("job scraped successfully", "job_id", jobs[i].ID)
					}
				}
			}
		}
	}
}

func (w *webrunner) scrapeJob(ctx context.Context, job *web.Job) error {
	job.Status = web.StatusWorking

	err := w.svc.Update(ctx, job)
	if err != nil {
		return err
	}

	if len(job.Data.Keywords) == 0 {
		job.Status = web.StatusFailed

		return w.svc.Update(ctx, job)
	}

	// Using RotatingCsvWriter for file rotation
	baseName := filepath.Join(w.cfg.DataFolder, job.ID)
	// Remove .csv extension if present (though created by join above without it usually)
	// but here job.ID is likely just UUID.

	// We initiate the rotating writer
	// It will create files like jobID_1.csv, jobID_2.csv, etc.
	rotatingWriter := NewRotatingCsvWriter(baseName, 50000)
	var countWg sync.WaitGroup
	rotatingWriter.OnWrite = func(amount int) {
		countWg.Add(1)
		go func() {
			defer countWg.Done()
			_ = w.svc.IncrementJobCount(context.Background(), job.ID, amount)
		}()
	}
	// rotatingWriter := NewRotatingCsvWriter(baseName, 10) // TEST LIMIT

	mate, err := w.setupMate(ctx, rotatingWriter, job)
	if err != nil {
		job.Status = web.StatusFailed
		_ = rotatingWriter.Close()

		err2 := w.svc.Update(ctx, job)
		if err2 != nil {
			logger.Error("failed to update job status", "error", err2)
		}

		return err
	}

	defer func() {
		_ = rotatingWriter.Close()
		_ = mate.Close()
	}()

	var coords string
	if job.Data.Lat != "" && job.Data.Lon != "" {
		coords = job.Data.Lat + "," + job.Data.Lon
	}

	dedup := deduper.New()
	exitMonitor := exiter.New()

	seedJobs, err := runner.CreateSeedJobs(
		job.Data.FastMode,
		job.Data.Lang,
		strings.NewReader(strings.Join(job.Data.Keywords, "\n")),
		job.Data.Depth,
		job.Data.Email,
		coords,
		job.Data.Zoom,
		func() float64 {
			if job.Data.Radius <= 0 {
				return 10000 // 10 km
			}

			return float64(job.Data.Radius)
		}(),
		dedup,
		exitMonitor,
		w.cfg.ExtraReviews,
		job.Data.SearchDelay,
	)
	if err != nil {
		err2 := w.svc.Update(ctx, job)
		if err2 != nil {
			logger.Error("failed to update job status", "error", err2)
		}

		return err
	}

	if len(seedJobs) > 0 {
		exitMonitor.SetSeedCount(len(seedJobs))

		// Calculate minimum required time based on actual job count and concurrency
		estimatedPerJob := 15 // seconds per job estimate for FastMode
		if !job.Data.FastMode {
			estimatedPerJob = 45 // normal mode with browser is slower
		}
		concurrency := max(1, w.cfg.Concurrency)
		minimumRequired := len(seedJobs)*estimatedPerJob*max(1, job.Data.Depth)/concurrency + 120
		if minimumRequired < 180 {
			minimumRequired = 180
		}

		allowedSeconds := minimumRequired

		if job.Data.MaxTime > 0 {
			userSeconds := int(job.Data.MaxTime.Seconds())
			if userSeconds < minimumRequired {
				logger.Warn("MaxTime too short for job count, auto-adjusting",
					"user_max_time", job.Data.MaxTime.String(),
					"calculated_minimum", time.Duration(minimumRequired)*time.Second,
					"seed_jobs", len(seedJobs),
					"concurrency", concurrency,
				)
				allowedSeconds = minimumRequired
			} else {
				allowedSeconds = userSeconds
			}
		}

		logger.Info("running job", "job_id", job.ID, "seed_jobs", len(seedJobs), "allowed_seconds", allowedSeconds, "concurrency", concurrency)

		mateCtx, cancel := context.WithTimeout(ctx, time.Duration(allowedSeconds)*time.Second)
		defer cancel()

		exitMonitor.SetCancelFunc(cancel)

		go exitMonitor.Run(mateCtx)

		err = mate.Start(mateCtx, seedJobs...)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			cancel()

			job.Status = web.StatusFailed
			err2 := w.svc.Update(ctx, job)
			if err2 != nil {
				logger.Error("failed to update job status", "error", err2)
			}

			return err
		}

		cancel()
	}

	// Explicitly close writer to flush all data (idempotent, safe with defer)
	_ = rotatingWriter.Close()

	// Wait for all pending count update goroutines to complete
	countWg.Wait()

	// Merge rotated CSV files into a single file and count actual rows
	actualCount, mergeErr := mergeCSVFiles(w.cfg.DataFolder, job.ID)
	if mergeErr != nil {
		logger.Warn("failed to merge CSV files", "error", mergeErr)
	}

	job.Count = actualCount
	job.Status = web.StatusOK

	return w.svc.Update(ctx, job)
}

func (w *webrunner) setupMate(_ context.Context, writer scrapemate.ResultWriter, job *web.Job) (*scrapemateapp.ScrapemateApp, error) {
	opts := []func(*scrapemateapp.Config) error{
		scrapemateapp.WithConcurrency(w.cfg.Concurrency),
		scrapemateapp.WithExitOnInactivity(time.Minute * 3),
	}

	if !job.Data.FastMode {
		opts = append(opts,
			scrapemateapp.WithJS(scrapemateapp.DisableImages()),
		)
	} else {
		opts = append(opts,
			scrapemateapp.WithStealth("firefox"),
		)
	}

	hasProxy := false

	if len(w.cfg.Proxies) > 0 {
		opts = append(opts, scrapemateapp.WithProxies(w.cfg.Proxies))
		hasProxy = true
	} else if len(job.Data.Proxies) > 0 {
		opts = append(opts,
			scrapemateapp.WithProxies(job.Data.Proxies),
		)
		hasProxy = true
	}

	if !w.cfg.DisablePageReuse {
		opts = append(opts,
			scrapemateapp.WithPageReuseLimit(50),
		)
	}

	logger.Info("job proxy status", "job_id", job.ID, "has_proxy", hasProxy)

	// writer is already a scrapemate.ResultWriter (RotatingCsvWriter)
	writers := []scrapemate.ResultWriter{writer}
	matecfg, err := scrapemateapp.NewConfig(
		writers,
		opts...,
	)
	if err != nil {
		return nil, err
	}

	return scrapemateapp.NewScrapeMateApp(matecfg)
}

// mergeCSVFiles merges all rotated CSV files ({jobID}_1.csv, {jobID}_2.csv, ...)
// into a single {jobID}.csv file and returns the actual data row count.
func mergeCSVFiles(dataFolder, jobID string) (int, error) {
	pattern := filepath.Join(dataFolder, jobID+"_*.csv")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return 0, err
	}

	sort.Strings(matches)

	outputPath := filepath.Join(dataFolder, jobID+".csv")
	outFile, err := os.Create(outputPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create merged file: %w", err)
	}
	defer outFile.Close()

	// Write BOM for Excel compatibility
	outFile.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(outFile)
	totalRows := 0
	headerWritten := false

	for _, match := range matches {
		records, readErr := readCSVWithBOM(match)
		if readErr != nil || len(records) == 0 {
			continue
		}

		startIdx := 0
		if !headerWritten {
			writer.Write(records[0])
			headerWritten = true
			startIdx = 1
		} else {
			startIdx = 1 // skip header from subsequent files
		}

		for i := startIdx; i < len(records); i++ {
			writer.Write(records[i])
			totalRows++
		}
	}

	writer.Flush()

	// Delete the rotated source files
	for _, match := range matches {
		os.Remove(match)
	}

	return totalRows, nil
}

// readCSVWithBOM reads a CSV file, stripping UTF-8 BOM if present.
func readCSVWithBOM(filePath string) ([][]string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Strip UTF-8 BOM if present
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}

	reader := csv.NewReader(strings.NewReader(string(data)))
	return reader.ReadAll()
}
