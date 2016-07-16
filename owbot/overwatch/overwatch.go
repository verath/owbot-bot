package overwatch

import (
	"encoding/json"
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/hashicorp/golang-lru"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// The base url of the owapi
	apiBaseUrl = "https://owapi.net/api/v2/"

	// The number of user stats entries to cache
	cacheSizeStats = 200

	// Time before user stats is considered stale and should be re-fetched
	cacheDurationStats = 5 * time.Minute

	// The timeout for use with the http client
	httpTimeout = 15 * time.Second
)

type UserStats struct {
	BattleTag    string `json:"battletag"`
	OverallStats struct {
		CompRank int `json:"comprank"`
		Games    int `json:"games"`
		Level    int `json:"level"`
		Losses   int `json:"losses"`
		Prestige int `json:"prestige"`
		Wins     int `json:"wins"`
		WinRate  int `json:"win_Rate"`
	} `json:"overall_stats"`
	GameStats struct {
		Deaths       float32 `json:"deaths"`
		Eliminations float32 `json:"eliminations"`
		SoloKills    float32 `json:"solo_kills"`
		KPD          float32 `json:"kpd"`
		TimePlayed   float32 `json:"time_played"`
	} `json:"game_stats"`
	Region string `json:"region"`
}

type userStatsCacheEntry struct {
	*UserStats
	addedAt time.Time
}

// ErrorResponse is an error that is populated with additional error
// data for the failed request.
// TODO: do we get any extra data on error?
type ErrorResponse struct {
	// The response that caused the error
	Response *http.Response
}

func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("%v %v: %d", e.Response.Request.Method, e.Response.Request.URL, e.Response.StatusCode)
}

type OverwatchClient struct {
	logger *logrus.Entry
	client *http.Client

	userStatsCache *lru.ARCCache

	baseUrl *url.URL

	// Mutex that must be held when sending a request to the api,
	// used so that we limit the amount of requests we do to a single
	// request at a time.
	reqMu sync.Mutex
}

// Creates a new OverwatchClient, a rest client for querying a third party
// overwatch api.
func NewOverwatchClient(logger *logrus.Logger) (*OverwatchClient, error) {
	userStatsCache, err := lru.NewARC(cacheSizeStats)
	if err != nil {
		return nil, err
	}

	// Store the logger as an Entry, adding the module to all log calls
	overwatchLogger := logger.WithField("module", "overwatch")
	client := &http.Client{Timeout: httpTimeout}
	baseUrl, _ := url.Parse(apiBaseUrl)

	return &OverwatchClient{
		logger:         overwatchLogger,
		client:         client,
		userStatsCache: userStatsCache,
		baseUrl:        baseUrl,
	}, nil
}

// Takes a response and returns an error if the status code is not within
// the 200-299 range.
func CheckResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return nil
	}
	return &ErrorResponse{Response: resp}
}

// Creates a new Request for the provided urlStr. The urlStr is resolved
// against baseUrl, and should not include a starting slash.
func (ow *OverwatchClient) NewRequest(urlStr string) (*http.Request, error) {
	ref, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	reqUrl := ow.baseUrl.ResolveReference(ref).String()
	req, err := http.NewRequest("GET", reqUrl, nil)
	if err != nil {
		return nil, err
	}
	return req, nil
}

// Do sends a request. If v is not nil, the response is treated as JSON and decoded to v.
// This method blocks until the request is sent and the response is received and parsed.
func (ow *OverwatchClient) Do(req *http.Request, v interface{}) (*http.Response, error) {
	resp, err := ow.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := resp.Body.Close(); err == nil {
			err = cerr
		}
	}()
	reqLogger := ow.logger.WithFields(logrus.Fields{"method": req.Method, "url": req.URL})

	err = CheckResponse(resp)
	if err != nil {
		reqLogger.WithError(err).Warn("Bad response")
		return nil, err
	}

	if v != nil {
		err = json.NewDecoder(resp.Body).Decode(v)
		if err != nil {
			errLogger := reqLogger.WithError(err)
			// We ignore UnmarshalTypeError errors, as returning the zero-value for the
			// field is better than returning nothing
			if _, ok := err.(*json.UnmarshalTypeError); ok {
				errLogger.Warn("Ignoring type error when decoding response as JSON")
			} else {
				errLogger.Error("Could not decode response as JSON")
				return nil, err
			}
		}
	}

	reqLogger.Debug("Request was successful")
	return resp, nil
}

// Returns a UserStats object for the provided BattleTag.
func (ow *OverwatchClient) GetStats(battleTag string) (*UserStats, error) {
	// Url friendly battleTag
	battleTag = strings.Replace(battleTag, "#", "-", -1)

	// Try get from cache, before obtaining the lock so that we can
	// return directly for cached requests
	if userStats, ok := ow.getUserStatsFromCache(battleTag); ok {
		return userStats, nil
	}

	path := fmt.Sprintf("u/%s/stats/competitive", battleTag)
	req, err := ow.NewRequest(path)
	if err != nil {
		return nil, err
	}

	// We obtain the single-request mutex here, so we don't spam the
	// OWAPI with requests
	// TODO: We might be stuck here for a long time, should add a deadline
	ow.reqMu.Lock()
	defer ow.reqMu.Unlock()

	// We check cache again after obtaining the lock, as we might
	// have slept during another request for the same battleTag
	if userStats, ok := ow.getUserStatsFromCache(battleTag); ok {
		return userStats, nil
	}

	userStats := &UserStats{}
	_, err = ow.Do(req, userStats)
	if err != nil {
		return nil, err
	}

	// Store to cache
	cacheEntry := userStatsCacheEntry{userStats, time.Now()}
	ow.userStatsCache.Add(battleTag, cacheEntry)

	return userStats, nil
}

// Returns a cached UserStats entry, if one exist and the data is not considered stale
func (ow *OverwatchClient) getUserStatsFromCache(battleTag string) (*UserStats, bool) {
	if cacheEntry, ok := ow.userStatsCache.Get(battleTag); ok {
		userStatsCacheEntry := cacheEntry.(userStatsCacheEntry)
		if time.Since(userStatsCacheEntry.addedAt) <= cacheDurationStats {
			return userStatsCacheEntry.UserStats, true
		}
	}
	return nil, false
}
