package limiter

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

//go:embed lua/rate_limit.lua
var rateLimitScript string

type RedisLimiter struct {
	client *redis.Client
	script *redis.Script
}

var (
	instance *RedisLimiter
	once     sync.Once
)

func New(ctx context.Context, r *redis.Client) *RedisLimiter {
	once.Do(func() {
		script := redis.NewScript(rateLimitScript)
		// 预加载脚本。加载失败不阻塞启动：Run 在脚本缓存丢失时会自动回退到 EVAL
		if _, err := script.Load(ctx, r).Result(); err != nil {
			common.SysLog(common.LogText("Failed to load rate limit script: %v", err))
		}
		instance = &RedisLimiter{
			client: r,
			script: script,
		}
	})

	return instance
}

func (rl *RedisLimiter) Allow(ctx context.Context, key string, opts ...Option) (bool, error) {
	// 默认配置
	config := &Config{
		Capacity:  10,
		Rate:      1,
		Requested: 1,
	}

	// 应用选项模式
	for _, opt := range opts {
		opt(config)
	}

	// 执行限流。Script.Run 先试 EVALSHA，遇到 NOSCRIPT 会自动回退 EVAL，
	// 因此 Redis 重启或脚本缓存被清空后限流不会持续失败
	result, err := rl.script.Run(
		ctx,
		rl.client,
		[]string{key},
		config.Requested,
		config.Rate,
		config.Capacity,
	).Int()

	if err != nil {
		if strings.HasPrefix(err.Error(), "NOSCRIPT ") {
			result, err := rl.client.Eval(
				ctx,
				rateLimitScript,
				[]string{key},
				config.Requested,
				config.Rate,
				config.Capacity,
			).Int()
			if err != nil {
				return false, fmt.Errorf("rate limit failed: %w", err)
			}
			return result == 1, nil
		}
		return false, fmt.Errorf("rate limit failed: %w", err)
	}
	return result == 1, nil
}

// Config 配置选项模式
type Config struct {
	Capacity  int64
	Rate      int64
	Requested int64
}

type Option func(*Config)

func WithCapacity(c int64) Option {
	return func(cfg *Config) { cfg.Capacity = c }
}

func WithRate(r int64) Option {
	return func(cfg *Config) { cfg.Rate = r }
}

func WithRequested(n int64) Option {
	return func(cfg *Config) { cfg.Requested = n }
}
