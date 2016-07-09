package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Sirupsen/logrus"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	API_BASE_URL = "https://discordapp.com/api/"

	// The default number of seconds to wait after getting a 429 - Too Many Requests
	// response. Used if the retry-after header could not be parsed
	DEFAULT_RETRY_AFTER = 30
)

type RestClient struct {
	logger *logrus.Entry
	client *http.Client

	// A mutex held when sending request, so that the rate of
	// requests can be controlled
	mu sync.Mutex

	rateLimitedUntil time.Time

	BaseUrl   *url.URL
	UserAgent string
	token     string
}

type ErrorResponse struct {
	// The response that caused the error
	Response *http.Response

	// https://discordapp.com/developers/docs/topics/response-codes#json-error-response
	Code int `json:"code"`

	// A more friendly error string
	Message string `json:"message"`
}

func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("%v %v: %d (%d) %v",
		e.Response.Request.Method, e.Response.Request.URL,
		e.Response.StatusCode,
		e.Code, e.Message)
}

func createErrorResponse(resp *http.Response) error {
	errorResponse := &ErrorResponse{Response: resp}
	err := json.NewDecoder(resp.Body).Decode(errorResponse)
	if err != nil {
		return err
	}
	return errorResponse
}

func NewRestClient(logger *logrus.Logger, token string, userAgent string) (*RestClient, error) {
	restLogger := logger.WithField("module", "discord-rest")

	baseUrl, _ := url.Parse(API_BASE_URL)
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	return &RestClient{
		client:    client,
		logger:    restLogger,
		token:     token,
		BaseUrl:   baseUrl,
		UserAgent: userAgent,
	}, nil
}

func (rc *RestClient) NewRequest(method string, urlStr string, body interface{}) (*http.Request, error) {
	// Resolve the urlStr against the base url
	ref, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	reqUrl := rc.BaseUrl.ResolveReference(ref).String()

	// Marshal the body as json if a body is present
	buf := new(bytes.Buffer)
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewBuffer(data)
	}

	req, err := http.NewRequest(method, reqUrl, buf)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", rc.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", rc.UserAgent)
	return req, nil
}

func (rc *RestClient) Do(req *http.Request, v interface{}) (*http.Response, error) {
	reqLogger := rc.logger.WithFields(logrus.Fields{
		"method": req.Method,
		"url":    req.URL,
	})

	// We lock here so that block additional requests if we
	// should limit our rate (i.e. if we got a 429 response)
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Wait until we "should" no longer be rate limited
	time.Sleep(rc.rateLimitedUntil.Sub(time.Now()))

	resp, err := rc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := resp.Body.Close(); err == nil {
			err = cerr
		}
	}()

	retryAfter := CheckRateLimited(resp)
	if retryAfter != nil {
		rc.rateLimitedUntil = time.Now().Add(*retryAfter)
		err := createErrorResponse(resp)
		reqLogger.WithFields(logrus.Fields{
			"retryAfter": retryAfter,
			"error":      err,
		}).Warn("Got a Too Many Requests response, limiting")
		return resp, err
	}

	err = CheckResponse(resp)
	if err != nil {
		reqLogger.WithField("error", err).Warn("Bad response")
		return nil, err
	}

	if v != nil {
		err = json.NewDecoder(resp.Body).Decode(v)
		if err != nil {
			reqLogger.WithField("error", err).Error("Could not decode response")
			return nil, err
		}
	}

	reqLogger.Debug("Request was successful")
	return resp, nil
}

func CheckRateLimited(resp *http.Response) *time.Duration {
	if resp.StatusCode != http.StatusTooManyRequests {
		return nil
	}
	retryAfter, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil {
		retryAfter = DEFAULT_RETRY_AFTER
	}
	retryAfterDur := time.Duration(retryAfter) * time.Millisecond
	return &retryAfterDur
}

func CheckResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return nil
	}
	return createErrorResponse(resp)
}

func (rc *RestClient) GetGateway() (*Gateway, error) {
	req, err := rc.NewRequest("GET", "gateway", nil)
	if err != nil {
		return nil, err
	}
	gateway := &Gateway{}
	_, err = rc.Do(req, gateway)
	return gateway, err
}

// https://discordapp.com/developers/docs/resources/channel#create-message
func (rc *RestClient) CreateMessage(channelId string, content string) error {
	path := fmt.Sprintf("channels/%s/messages", channelId)
	body := struct {
		Content string `json:"content"`
	}{content}

	req, err := rc.NewRequest("POST", path, body)
	if err != nil {
		return err
	}

	_, err = rc.Do(req, nil)
	return err
}
