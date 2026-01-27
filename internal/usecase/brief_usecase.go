package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"go-nexus/internal/domain"

	"github.com/redis/go-redis/v9"
)

type BriefUseCase struct {
	repo domain.BriefRepository
	rdb  *redis.Client
}

func NewBriefUseCase(repo domain.BriefRepository, rdb *redis.Client) *BriefUseCase {
	return &BriefUseCase{repo: repo, rdb: rdb}
}

func (uc *BriefUseCase) GetMorningBrief(ctx context.Context, date string) ([]domain.MorningBrief, error) {
	// 1. Try Redis first
	if date == "" {
		// Try to get latest date from cache
		val, err := uc.rdb.Get(ctx, "brief:latest_date").Result()
		if err == nil && val != "" {
			date = val
		}
	}

	// Only try Redis if we have a date (either passed in or found in latest_date)
	if date != "" {
		// Get index
		indexKey := fmt.Sprintf("brief:index:%s", date)
		tickers, err := uc.rdb.SMembers(ctx, indexKey).Result()
		if err == nil && len(tickers) > 0 {
			var keys []string
			for _, t := range tickers {
				keys = append(keys, fmt.Sprintf("brief:object:%s:%s", date, t))
			}
			// MGET
			vals, err := uc.rdb.MGet(ctx, keys...).Result()
			if err == nil {
				var result []domain.MorningBrief
				for _, v := range vals {
					if vStr, ok := v.(string); ok {
						var b domain.MorningBrief
						if json.Unmarshal([]byte(vStr), &b) == nil {
							result = append(result, b)
						}
					}
				}
				if len(result) > 0 {
					return result, nil
				}
			}
		}
	}

	// 2. Fallback to DB
	return uc.repo.GetLatest(date)
}
