package accounting

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/songzhibin97/cc-gateway/internal/store"
)

type Cleaner struct {
	repo          *store.LogRepo
	retentionDays int
	interval      time.Duration
	logger        *slog.Logger
	done          chan struct{}
	stopOnce      sync.Once
}

func NewCleaner(repo *store.LogRepo, retentionDays int, interval time.Duration, logger *slog.Logger) *Cleaner {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = time.Hour
	}

	c := &Cleaner{
		repo:          repo,
		retentionDays: retentionDays,
		interval:      interval,
		logger:        logger,
		done:          make(chan struct{}),
	}
	go c.loop()
	return c
}

func (c *Cleaner) loop() {
	c.clean()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.clean()
		case <-c.done:
			return
		}
	}
}

func (c *Cleaner) CleanNow() (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deleted, err := c.repo.CleanPayloads(ctx, c.retentionDays)
	if err != nil {
		c.logger.Error("payload cleanup failed", slog.String("error", err.Error()))
		return 0, err
	}
	if deleted > 0 {
		c.logger.Info("cleaned old payloads", slog.Int64("deleted", deleted))
	}
	return deleted, nil
}

func (c *Cleaner) clean() {
	_, _ = c.CleanNow()
}

func (c *Cleaner) Stop() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		close(c.done)
	})
}
