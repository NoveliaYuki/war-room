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

// ListSearchPeriods returns the search periods available to userID.
func (s *JobService) ListSearchPeriods(userID string) ([]models.SearchPeriod, error) {
	return s.repo.ListSearchPeriods(userID)
}

// CreateSearchPeriod validates and stores a dated search period for userID.
func (s *JobService) CreateSearchPeriod(userID string, input models.SearchPeriodInput) (*models.SearchPeriod, error) {
	period, err := normalizeSearchPeriod(input)
	if err != nil {
		return nil, err
	}
	period.ID = uuid.NewString()
	if err := s.repo.CreateSearchPeriod(userID, period); err != nil {
		return nil, mapSearchPeriodError(err)
	}
	s.mirrorAfterMutation()
	return period, nil
}

// UpdateSearchPeriod changes a period while retaining all job assignments, if owned by userID.
func (s *JobService) UpdateSearchPeriod(userID, id string, input models.SearchPeriodInput) error {
	period, err := normalizeSearchPeriod(input)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateSearchPeriod(userID, id, models.SearchPeriodInput{
		Name: period.Name, StartDate: period.StartDate, EndDate: period.EndDate,
	}); err != nil {
		return mapSearchPeriodError(err)
	}
	s.mirrorAfterMutation()
	return nil
}

// DeleteSearchPeriod removes a period owned by userID and leaves its jobs unassigned.
func (s *JobService) DeleteSearchPeriod(userID, id string) error {
	if err := s.repo.DeleteSearchPeriod(userID, id); err != nil {
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

func (s *JobService) resolveJobSearchPeriod(userID string, input *string) (*string, error) {
	if input == nil {
		date := time.Now().Format("2006-01-02")
		period, err := s.repo.SearchPeriodForDate(userID, date)
		if err != nil || period == nil {
			return nil, err
		}
		return &period.ID, nil
	}
	if strings.TrimSpace(*input) == "" {
		return nil, nil
	}
	period, err := s.repo.GetSearchPeriod(userID, strings.TrimSpace(*input))
	if err != nil {
		return nil, err
	}
	if period == nil {
		return nil, fmt.Errorf("%w: search period does not exist", ErrInvalidField)
	}
	return &period.ID, nil
}

func (s *JobService) resolveNewJobSearchPeriod(userID string, input *string) (*string, error) {
	if input != nil && strings.TrimSpace(*input) != "" {
		return s.resolveJobSearchPeriod(userID, input)
	}
	return s.resolveJobSearchPeriod(userID, nil)
}

func (s *JobService) validateJobSearchPeriod(userID string, input *string) error {
	if input == nil {
		return nil
	}
	_, err := s.resolveJobSearchPeriod(userID, input)
	return err
}
