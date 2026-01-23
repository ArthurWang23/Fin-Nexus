package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"go-nexus/internal/domain"
	"time"
)

type RedisChatRepo struct {
	rdb *redis.Client
}

func NewRedisChatRepo(rdb *redis.Client) *RedisChatRepo {
	return &RedisChatRepo{rdb: rdb}
}

func (r *RedisChatRepo) key(sessionID string) string {
	return fmt.Sprintf("chat:history:%s", sessionID)
}

func (r *RedisChatRepo) AddMessage(ctx context.Context, sessionID string, msg domain.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	k := r.key(sessionID)
	if err := r.rdb.RPush(ctx, k, data).Err(); err != nil {
		return err
	}
	// 7天不说话就遗忘
	r.rdb.Expire(ctx, k, 7*24*time.Hour)
	// 滑动窗口只保留20条对话（10轮对话）
	r.rdb.LTrim(ctx, k, -20, -1)
	return nil
}

func (r *RedisChatRepo) GetHistory(ctx context.Context, sessionID string, limit int) ([]domain.Message, error) {
	k := r.key(sessionID)
	// 获取所有历史 (因为 Add 的时候已经 Trim 过了，这里取全部即可)
	result, err := r.rdb.LRange(ctx, k, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	var messages []domain.Message
	for _, item := range result {
		var msg domain.Message
		if err := json.Unmarshal([]byte(item), &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func (r *RedisChatRepo) Clear(ctx context.Context, sessionID string) error {
	return r.rdb.Del(ctx, r.key(sessionID)).Err()
}
