package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"war-room/backend/pkg/models"
	"war-room/backend/pkg/repository"
)

// ListSearchPeriods returns the available search periods.
func (s *JobService) ListSearchPeriods() ([]models.SearchPeriod, error) {
	return s.repo.ListSearchPeriods()
}

// CreateSearchPeriod validates and stores a dated search period.
func (s *JobService) CreateSearchPeriod(input models.SearchPeriodInput) (*models.SearchPeriod, error) {
	period, err := normalizeSearchPeriod(input)
	if err != nil {
		return nil, err
	}
	period.ID = uuid.NewString()
	if err := s.repo.CreateSearchPeriod(period); err != nil {
		return nil, mapSearchPeriodError(err)
	}
	s.mirrorAfterMutation()
	return period, nil
}

// UpdateSearchPeriod changes a period while retaining all job assignments.
func (s *JobService) UpdateSearchPeriod(id string, input models.SearchPeriodInput) error {
	period, err := normalizeSearchPeriod(input)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateSearchPeriod(id, models.SearchPeriodInput{
		Name: period.Name, StartDate: period.StartDate, EndDate: period.EndDate,
	}); err != nil {
		return mapSearchPeriodError(err)
	}
	s.mirrorAfterMutation()
	return nil
}

// DeleteSearchPeriod removes a period and leaves its jobs unassigned.
func (s *JobService) DeleteSearchPeriod(id string) error {
	if err := s.repo.DeleteSearchPeriod(id); err != nil {
		return err
	}
	s.mirrorAfterMutation()
	return nil
}

func normalizeSearchPeriod(input models.SearchPeriodInput) (*models.SearchPeriod, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: period name must contain 1 to 100 characters", ErrInvalidField)
	}
	start, err := parseSearchPeriodDate(input.StartDate)
	if err != nil {
		return nil, err
	}
	if input.EndDate != "" {
		end, err := parseSearchPeriodDate(input.EndDate)
		if err != nil {
			return nil, err
		}
		if end.Before(start) {
			return nil, fmt.Errorf("%w: end date must be on or after start date", ErrInvalidField)
		}
	}
	return &models.SearchPeriod{Name: name, StartDate: input.StartDate, EndDate: input.EndDate}, nil
}

func parseSearchPeriodDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("%w: dates must be valid YYYY-MM-DD calendar dates", ErrInvalidField)
	}
	return parsed, nil
}

func mapSearchPeriodError(err error) error {
	if errors.Is(err, repository.ErrSearchPeriodOverlap) {
		return fmt.Errorf("%w: search period dates overlap an existing period", ErrInvalidField)
	}
	return err
}

func (s *JobService) resolveJobSearchPeriod(input *string) (*string, error) {
	if input == nil {
		date := time.Now().Format("2006-01-02")
		period, err := s.repo.SearchPeriodForDate(date)
		if err != nil || period == nil {
			return nil, err
		}
		return &period.ID, nil
	}
	if strings.TrimSpace(*input) == "" {
		return nil, nil
	}
	period, err := s.repo.GetSearchPeriod(strings.TrimSpace(*input))
	if err != nil {
		return nil, err
	}
	if period == nil {
		return nil, fmt.Errorf("%w: search period does not exist", ErrInvalidField)
	}
	return &period.ID, nil
}

func (s *JobService) validateJobSearchPeriod(input *string) error {
	if input == nil {
		return nil
	}
	_, err := s.resolveJobSearchPeriod(input)
	return err
}
