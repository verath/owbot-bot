package overwatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/hashicorp/golang-lru"
	"net/http"
	"strings"
	"time"
)

const (
	API_BASE_URL = "https://owapi.net/api/v2"

	// The number of user stats entries to cache
	USER_STATS_CACHE_SIZE = 200

	// Time before user stats is considered stale and should be refetched
	USER_STATS_CACHE_DURATION = 15 * time.Minute
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

type OverwatchClient struct {
	logger         *logrus.Entry
	userStatsCache *lru.ARCCache
}

func (ow *OverwatchClient) GetStats(battleTag string) (*UserStats, error) {
	// Url friendly battleTag
	battleTag = strings.Replace(battleTag, "#", "-", -1)

	if userStats, ok := ow.getUserStatsFromCache(battleTag); ok {
		return userStats, nil
	}

	url := API_BASE_URL
	url += fmt.Sprintf("/u/%s/stats/general", battleTag)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		ow.logger.WithFields(logrus.Fields{
			"status": resp.StatusCode,
			"url":    url,
		}).Warn("Got a non-200 response from API")
		return nil, errors.New("Non-200 response")
	}

	userStats := UserStats{}
	err = json.NewDecoder(resp.Body).Decode(&userStats)
	if err != nil {
		return nil, err
	}

	// Store to cache
	cacheEntry := userStatsCacheEntry{&userStats, time.Now()}
	ow.userStatsCache.Add(battleTag, cacheEntry)

	return &userStats, nil
}

// Returns a cached UserStats entry, if one exist and the data is not considered stale
func (ow *OverwatchClient) getUserStatsFromCache(battleTag string) (*UserStats, bool) {
	if cacheEntry, ok := ow.userStatsCache.Get(battleTag); ok {
		userStatsCacheEntry := cacheEntry.(userStatsCacheEntry)
		if time.Since(userStatsCacheEntry.addedAt) <= USER_STATS_CACHE_DURATION {
			return userStatsCacheEntry.UserStats, true
		}
	}
	return nil, false
}

func NewOverwatch(logger *logrus.Logger) (*OverwatchClient, error) {
	userStatsCache, err := lru.NewARC(USER_STATS_CACHE_SIZE)
	if err != nil {
		return nil, err
	}

	// Store the logger as an Entry, adding the module to all log calls
	overwatchLogger := logger.WithField("module", "overwatch")

	return &OverwatchClient{
		logger:         overwatchLogger,
		userStatsCache: userStatsCache,
	}, nil
}
