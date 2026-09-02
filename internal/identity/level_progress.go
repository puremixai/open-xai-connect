package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store"
)

type LevelProgressSnapshot struct {
	SchemaVersion      int                      `json:"schema_version"`
	DiscourseID        int64                    `json:"discourse_id"`
	CurrentLevel       LevelInfo                `json:"current_level"`
	NextLevel          *LevelInfo               `json:"next_level"`
	PromotionMode      string                   `json:"promotion_mode"`
	RequirementsMet    *bool                    `json:"requirements_met"`
	Requirements       []LevelRequirement       `json:"requirements"`
	BlockingConditions []LevelBlockingCondition `json:"blocking_conditions"`
	GeneratedAt        time.Time                `json:"generated_at"`
}

type LevelInfo struct {
	ID    int    `json:"id"`
	Key   string `json:"key"`
	Label string `json:"label"`
}

type LevelRequirement struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Group      string `json:"group"`
	Scope      string `json:"scope"`
	PeriodDays *int   `json:"period_days"`
	Current    int64  `json:"current"`
	Target     int64  `json:"target"`
	Operator   string `json:"operator"`
	Unit       string `json:"unit"`
	Met        bool   `json:"met"`
}

type LevelBlockingCondition struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Met   bool   `json:"met"`
}

type LevelProgressProvider interface {
	FetchLevelProgress(context.Context, int64) (LevelProgressSnapshot, error)
}

type LevelProgressLookup interface {
	CurrentLevelProgress(context.Context, string) (LevelProgressSnapshot, error)
}

type LevelProgressCache interface {
	Get(context.Context, int64) (LevelProgressSnapshot, bool, error)
	Set(context.Context, LevelProgressSnapshot, time.Duration) error
	Invalidate(context.Context, int64) error
}

type LevelProgressRefresher struct {
	provider LevelProgressProvider
	users    store.UserRepository
	cache    LevelProgressCache
}

func NewLevelProgressRefresher(provider LevelProgressProvider, users store.UserRepository, cache LevelProgressCache) *LevelProgressRefresher {
	return &LevelProgressRefresher{provider: provider, users: users, cache: cache}
}

func (r *LevelProgressRefresher) CurrentLevelProgress(ctx context.Context, subject string) (LevelProgressSnapshot, error) {
	if r == nil || r.provider == nil || r.users == nil {
		return LevelProgressSnapshot{}, errors.New("level progress refresher is not initialized")
	}
	user, err := r.users.GetBySubject(ctx, domain.UserID(subject))
	if err != nil {
		return LevelProgressSnapshot{}, err
	}
	if r.cache != nil {
		snapshot, ok, err := r.cache.Get(ctx, user.DiscourseID)
		if err != nil {
			return LevelProgressSnapshot{}, err
		}
		if ok {
			return snapshot, nil
		}
	}
	snapshot, err := r.provider.FetchLevelProgress(ctx, user.DiscourseID)
	if err != nil {
		return LevelProgressSnapshot{}, err
	}
	if err := snapshot.ValidateFor(user.DiscourseID); err != nil {
		return LevelProgressSnapshot{}, err
	}
	if r.cache != nil {
		if err := r.cache.Set(ctx, snapshot, 0); err != nil {
			return LevelProgressSnapshot{}, err
		}
	}
	return snapshot, nil
}

func (s LevelProgressSnapshot) ValidateFor(discourseID int64) error {
	if s.SchemaVersion != 1 {
		return fmt.Errorf("unsupported level progress schema version %d", s.SchemaVersion)
	}
	if s.DiscourseID != discourseID {
		return fmt.Errorf("level progress discourse id mismatch: got %d want %d", s.DiscourseID, discourseID)
	}
	if err := validateLevelInfo(s.CurrentLevel); err != nil {
		return fmt.Errorf("invalid current level: %w", err)
	}
	if s.CurrentLevel.ID < 0 || s.CurrentLevel.ID > 4 {
		return fmt.Errorf("current level %d is out of range", s.CurrentLevel.ID)
	}
	if err := validatePromotionMode(s.PromotionMode); err != nil {
		return err
	}
	switch s.PromotionMode {
	case "automatic":
		if s.RequirementsMet == nil {
			return errors.New("automatic level progress requires requirements_met")
		}
	case "manual", "locked", "none":
		if s.RequirementsMet != nil {
			return fmt.Errorf("%s level progress must not include requirements_met", s.PromotionMode)
		}
	}
	if len(s.Requirements) > 64 {
		return fmt.Errorf("level progress has %d requirements; max is 64", len(s.Requirements))
	}
	for i, requirement := range s.Requirements {
		if err := validateLevelRequirement(requirement); err != nil {
			return fmt.Errorf("invalid requirement %d: %w", i, err)
		}
	}
	for i, condition := range s.BlockingConditions {
		if err := validateBlockingCondition(condition); err != nil {
			return fmt.Errorf("invalid blocking condition %d: %w", i, err)
		}
	}
	if s.CurrentLevel.ID == 4 {
		if s.NextLevel != nil {
			return errors.New("level 4 must not include next_level")
		}
		return nil
	}
	if s.NextLevel == nil {
		return errors.New("non-terminal level progress must include next_level")
	}
	if s.NextLevel.ID != s.CurrentLevel.ID+1 {
		return fmt.Errorf("next level id %d does not match current level %d", s.NextLevel.ID, s.CurrentLevel.ID)
	}
	if s.CurrentLevel.ID == 3 && s.NextLevel.ID == 4 && s.PromotionMode != "manual" {
		return errors.New("level 3 to 4 promotion must use manual promotion mode")
	}
	if err := validateLevelInfo(*s.NextLevel); err != nil {
		return fmt.Errorf("invalid next level: %w", err)
	}
	return nil
}

func validateLevelInfo(info LevelInfo) error {
	if info.ID < 0 {
		return fmt.Errorf("level id %d must be non-negative", info.ID)
	}
	if strings.TrimSpace(info.Key) == "" {
		return errors.New("level key must not be empty")
	}
	if strings.TrimSpace(info.Label) == "" {
		return errors.New("level label must not be empty")
	}
	return nil
}

func validatePromotionMode(mode string) error {
	switch mode {
	case "automatic", "manual", "locked", "none":
		return nil
	default:
		return fmt.Errorf("unknown promotion mode %q", mode)
	}
}

func validateLevelRequirement(requirement LevelRequirement) error {
	if strings.TrimSpace(requirement.Key) == "" {
		return errors.New("requirement key must not be empty")
	}
	if strings.TrimSpace(requirement.Label) == "" {
		return errors.New("requirement label must not be empty")
	}
	switch requirement.Scope {
	case "account_lifetime", "rolling_period", "all_time":
	default:
		return fmt.Errorf("unsupported scope %q", requirement.Scope)
	}
	switch requirement.Operator {
	case "at_least", "at_most", "equals":
	default:
		return fmt.Errorf("unsupported operator %q", requirement.Operator)
	}
	switch requirement.Unit {
	case "count", "days", "minutes":
	default:
		return fmt.Errorf("unsupported unit %q", requirement.Unit)
	}
	if requirement.Current < 0 {
		return errors.New("current value must be non-negative")
	}
	if requirement.Target < 0 {
		return errors.New("target value must be non-negative")
	}
	if requirement.PeriodDays != nil && *requirement.PeriodDays < 0 {
		return errors.New("period_days must be non-negative")
	}
	return nil
}

func validateBlockingCondition(condition LevelBlockingCondition) error {
	if strings.TrimSpace(condition.Key) == "" {
		return errors.New("blocking condition key must not be empty")
	}
	if strings.TrimSpace(condition.Label) == "" {
		return errors.New("blocking condition label must not be empty")
	}
	return nil
}
