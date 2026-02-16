package web

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"
)

type Service struct {
	repo       JobRepository
	dataFolder string
}

func NewService(repo JobRepository, dataFolder string) *Service {
	return &Service{
		repo:       repo,
		dataFolder: dataFolder,
	}
}

func (s *Service) Create(ctx context.Context, job *Job) error {
	return s.repo.Create(ctx, job)
}

func (s *Service) All(ctx context.Context) ([]Job, error) {
	return s.repo.Select(ctx, SelectParams{})
}

func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return fmt.Errorf("invalid file name")
	}

	// Delete merged CSV
	os.Remove(filepath.Join(s.dataFolder, id+".csv"))

	// Delete rotated CSV files ({id}_1.csv, {id}_2.csv, ...)
	csvPattern := filepath.Join(s.dataFolder, id+"_*.csv")
	if matches, err := filepath.Glob(csvPattern); err == nil {
		for _, m := range matches {
			os.Remove(m)
		}
	}

	// Delete Excel files
	xlsxPattern := filepath.Join(s.dataFolder, id+"*.xlsx")
	if matches, err := filepath.Glob(xlsxPattern); err == nil {
		for _, m := range matches {
			os.Remove(m)
		}
	}

	return s.repo.Delete(ctx, id)
}

func (s *Service) Update(ctx context.Context, job *Job) error {
	return s.repo.Update(ctx, job)
}

func (s *Service) SelectPending(ctx context.Context) ([]Job, error) {
	return s.repo.Select(ctx, SelectParams{Status: StatusPending, Limit: 1})
}

func (s *Service) GetCSV(_ context.Context, id string) (string, error) {
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return "", fmt.Errorf("invalid file name")
	}

	// First check for merged CSV file
	datapath := filepath.Join(s.dataFolder, id+".csv")
	if _, err := os.Stat(datapath); err == nil {
		return datapath, nil
	}

	// Then check for rotated CSV files ({id}_1.csv, {id}_2.csv, ...)
	pattern := filepath.Join(s.dataFolder, id+"_*.csv")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("error searching for csv files: %w", err)
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("csv file not found for job %s", id)
	}

	sort.Strings(matches)
	return matches[0], nil
}

func (s *Service) GetExcel(ctx context.Context, id string, fields []string) (string, error) {
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return "", fmt.Errorf("invalid file name")
	}

	csvPath, err := s.GetCSV(ctx, id)
	if err != nil {
		return "", fmt.Errorf("failed to find csv: %w", err)
	}

	// Read CSV
	csvFile, err := os.Open(csvPath)
	if err != nil {
		return "", fmt.Errorf("failed to open csv: %w", err)
	}
	defer csvFile.Close()

	reader := csv.NewReader(csvFile)
	records, err := reader.ReadAll()
	if err != nil {
		return "", fmt.Errorf("failed to read csv: %w", err)
	}

	// Filter records if fields specified
	if len(fields) > 0 && len(records) > 0 {
		records = filterRecords(records, fields)
	}

	// Create Excel
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	// Write data
	for i, row := range records {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		if err != nil {
			return "", err
		}
		if err := f.SetSheetRow("Sheet1", cell, &row); err != nil {
			return "", err
		}
	}

	// Save Excel (always generate fresh to respect current field selection)
	suffix := ""
	if len(fields) > 0 {
		suffix = "_filtered"
	}
	xlsxPath := filepath.Join(s.dataFolder, id+suffix+".xlsx")

	if err := f.SaveAs(xlsxPath); err != nil {
		return "", fmt.Errorf("failed to save xlsx: %w", err)
	}

	return xlsxPath, nil
}

func (s *Service) FilterCSV(csvPath string, fields []string) ([]byte, error) {
	csvFile, err := os.Open(csvPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open csv: %w", err)
	}
	defer csvFile.Close()

	reader := csv.NewReader(csvFile)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read csv: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("empty csv file")
	}

	filtered := filterRecords(records, fields)

	var buf strings.Builder
	writer := csv.NewWriter(&buf)
	if err := writer.WriteAll(filtered); err != nil {
		return nil, err
	}
	writer.Flush()

	return []byte(buf.String()), nil
}

func filterRecords(records [][]string, fields []string) [][]string {
	if len(records) == 0 {
		return records
	}

	headers := records[0]

	// Find column indices for requested fields
	fieldSet := make(map[string]bool)
	for _, f := range fields {
		fieldSet[strings.TrimSpace(f)] = true
	}

	var indices []int
	for i, h := range headers {
		if fieldSet[h] {
			indices = append(indices, i)
		}
	}

	if len(indices) == 0 {
		return records // no matching fields, return all
	}

	var result [][]string
	for _, row := range records {
		var filteredRow []string
		for _, idx := range indices {
			if idx < len(row) {
				filteredRow = append(filteredRow, row[idx])
			}
		}
		result = append(result, filteredRow)
	}

	return result
}
func (s *Service) GetJobCount(ctx context.Context, id string) (int, error) {
	job, err := s.repo.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	return job.Count, nil
}

func (s *Service) IncrementJobCount(ctx context.Context, id string, amount int) error {
	return s.repo.IncrementCount(ctx, id, amount)
}
