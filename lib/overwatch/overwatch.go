package overwatch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	API_BASE_URL         = "https://owapi.net/api/v2"
	USER_STATS_CACHE_TTL = 10 * time.Minute
)

type UserStats struct {
	BattleTag    string `json:"battletag"`
	OverallStats struct {
		CompRank int `json:"comprank"`
		Game     int `json:"games"`
		Level    int `json:"level"`
		Losses   int `json:"losses"`
		Wins     int `json:"wins"`
		WinRate  int `json:"win_Rate"`
	} `json:"overall_stats"`
	Region string `json:"region"`
}

type userStatsCacheEntry struct {
	*UserStats
	addedAt time.Time
}

type Overwatch struct {
	sync.RWMutex

	userStatsCache map[string]userStatsCacheEntry
}

func (o *Overwatch) getStatsFromCache(battleTag string) (*UserStats, bool) {
	o.RLock()
	defer o.RUnlock()

	cacheEntry, exist := o.userStatsCache[battleTag]
	if !exist {
		return nil, false
	}
	if time.Since(cacheEntry.addedAt) > USER_STATS_CACHE_TTL {
		return nil, false
	}
	return cacheEntry.UserStats, true
}

func (o *Overwatch) GetStats(battleTag string) (*UserStats, error) {
	// Url friendly battleTag
	battleTag = strings.Replace(battleTag, "#", "-", -1)

	if userStats, ok := o.getStatsFromCache(battleTag); ok {
		return userStats, nil
	}

	url := API_BASE_URL
	url += fmt.Sprintf("/u/%s/stats/general", battleTag)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	userStats := UserStats{}
	err = json.NewDecoder(resp.Body).Decode(&userStats)
	if err != nil {
		return nil, err
	}

	// Store to cache
	o.Lock()
	o.userStatsCache[battleTag] = userStatsCacheEntry{&userStats, time.Now()}
	o.Unlock()

	return &userStats, nil
}

func NewOverwatch() Overwatch {
	return Overwatch{
		userStatsCache: make(map[string]userStatsCacheEntry),
	}
}
