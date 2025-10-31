package trader

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tomikartemik/crypto_bot/internal/log"
)

type tradeStats struct {
	InitialBalance float64 `json:"initial_balance"`
	LastBalance    float64 `json:"last_balance"`
	TotalWins      int     `json:"total_wins"`
	TotalLosses    int     `json:"total_losses"`
	DailyWins      int     `json:"daily_wins"`
	DailyLosses    int     `json:"daily_losses"`
	LastReportDate string  `json:"last_report_date"`
}

type StatsManager struct {
	mu    sync.Mutex
	path  string
	stats tradeStats
}

var (
	statsOnce     sync.Once
	statsInstance *StatsManager
	statsInitErr  error
)

func initStatsManager(path string, initialBalance float64) (*StatsManager, error) {
	statsOnce.Do(func() {
		if path == "" {
			path = "trade_stats.json"
		}
		statsInstance, statsInitErr = loadOrCreateStats(path, initialBalance)
	})
	if statsInitErr != nil {
		return nil, statsInitErr
	}
	return statsInstance, nil
}

func loadOrCreateStats(path string, initialBalance float64) (*StatsManager, error) {
	var stats tradeStats

	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		stats = newStats(initialBalance)
		if err := writeStats(path, stats); err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(data, &stats); err != nil {
			return nil, fmt.Errorf("не удалось разобрать %s: %w", path, err)
		}
		if stats.InitialBalance == 0 {
			stats.InitialBalance = initialBalance
		}
		if stats.LastBalance == 0 {
			stats.LastBalance = stats.InitialBalance
		}
	}

	return &StatsManager{path: path, stats: stats}, nil
}

func newStats(initialBalance float64) tradeStats {
	if initialBalance == 0 {
		initialBalance = 50
	}
	return tradeStats{
		InitialBalance: initialBalance,
		LastBalance:    initialBalance,
	}
}

func writeStats(path string, stats tradeStats) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (m *StatsManager) RecordTrade(win bool) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if win {
		m.stats.DailyWins++
	} else {
		m.stats.DailyLosses++
	}
	if err := writeStats(m.path, m.stats); err != nil {
		log.Log.Error("Не удалось сохранить статистику", "error", err)
	}
}

func (m *StatsManager) BuildDailyReport(now time.Time, balance float64, currency string) (string, bool, error) {
	if m == nil {
		return "", false, nil
	}

	dateStr := now.Format("2006-01-02")

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stats.LastReportDate == dateStr {
		return "", false, nil
	}

	dayChange := balance - m.stats.LastBalance
	totalChange := balance - m.stats.InitialBalance

	message := fmt.Sprintf(
		"📊 *Итог дня — %s*\n\n"+
			"💼 Баланс: %.2f %s\n"+
			"  • За день: %s\n"+
			"  • Всего: %s\n\n"+
			"Трейды за день: ✅ %d | ❌ %d\n"+
			"Всего трейдов: ✅ %d | ❌ %d",
		now.Format("02.01.2006"),
		balance,
		currency,
		formatDelta(dayChange, currency),
		formatDelta(totalChange, currency),
		m.stats.DailyWins,
		m.stats.DailyLosses,
		m.stats.TotalWins+m.stats.DailyWins,
		m.stats.TotalLosses+m.stats.DailyLosses,
	)

	m.stats.TotalWins += m.stats.DailyWins
	m.stats.TotalLosses += m.stats.DailyLosses
	m.stats.LastBalance = balance
	m.stats.DailyWins = 0
	m.stats.DailyLosses = 0
	m.stats.LastReportDate = dateStr

	if err := writeStats(m.path, m.stats); err != nil {
		log.Log.Error("Не удалось обновить статистику по окончании дня", "error", err)
		return "", false, err
	}

	return message, true, nil
}

func formatDelta(delta float64, currency string) string {
	sign := ""
	if delta > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.2f %s", sign, delta, currency)
}
