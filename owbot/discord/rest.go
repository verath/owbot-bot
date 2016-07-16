package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Sirupsen/logrus"
	"golang.org/x/net/context"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	apiBaseURL = "https://discordapp.com/api/"

	// The default number of seconds to wait after getting a 429 - Too Many Requests
	// response. Used if the retry-after header could not be parsed
	defaultRetryAfter = 10 * time.Second
)

type RestClient struct {
	logger *logrus.Entry
	client *http.Client

	// A mutex held when sending request, so that the rate of
	// requests can be controlled
	mu sync.Mutex

	baseURL   *url.URL
	userAgent string
	token     string
}

// RequestCreatorFunc is a wrapper around a function creating an http
// request. This is used so that a request can be sent multiple times,
// as needed when a request is failed due to a 429 error.
type RequestCreatorFunc func() (*http.Request, error)

// ErrorResponse is an error that is populated with additional Discord error
// data for the failed request.
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

// Creates an ErrorResponse from a (failed) HTTP Response
// Tries to populate the ErrorResponse with additional data from
// the response, if available.
func createErrorResponse(resp *http.Response) error {
	errorResponse := &ErrorResponse{Response: resp}
	body, err := ioutil.ReadAll(resp.Body)
	if err == nil && len(body) > 0 {
		if err := json.Unmarshal(body, errorResponse); err != nil {
			return err
		}
	}
	return errorResponse
}

func NewRestClient(logger *logrus.Logger, token string, userAgent string) (*RestClient, error) {
	restLogger := logger.WithField("module", "discord-rest")

	baseURL, _ := url.Parse(apiBaseURL)
	client := &http.Client{Timeout: 30 * time.Second}

	return &RestClient{
		client:    client,
		logger:    restLogger,
		token:     token,
		baseURL:   baseURL,
		userAgent: userAgent,
	}, nil
}

// Creates a new Request from the provided parameters. The urlStr is resolved
// against the BaseUrl, and should not include a starting slash. The body,
// if not nil, is encoded as JSON.
func (rc *RestClient) NewRequest(ctx context.Context, method string, urlStr string, body interface{}) (*http.Request, error) {
	// Check the context to make sure it is not canceled already
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Resolve the urlStr against the base url
	ref, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	reqURL := rc.baseURL.ResolveReference(ref).String()

	// Marshal the body as json if a body is present
	buf := new(bytes.Buffer)
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewBuffer(data)
	}

	req, err := http.NewRequest(method, reqURL, buf)
	if err != nil {
		return nil, err
	}

	// Set request to cancel when the context is canceled
	req.Cancel = ctx.Done()

	req.Header.Set("Authorization", rc.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", rc.userAgent)
	return req, nil
}

// Wrapper that returns a RequestCreatorFunc that calls NewRequest with the provided parameters
func (rc *RestClient) NewRequestCreatorFunc(ctx context.Context, method string, urlStr string, body interface{}) RequestCreatorFunc {
	return func() (*http.Request, error) {
		return rc.NewRequest(ctx, method, urlStr, body)
	}
}

// Do sends a request. If v is not nil, the response is treated as JSON and decoded to v.
// Failed requests due to 429 errors are retried until they pass (or fail for other reasons).
// This method blocks until it can obtain the single send mutex, and until the request is sent
// and the response is received and parsed. As such, it may block for a long time.
func (rc *RestClient) Do(reqFunc RequestCreatorFunc, v interface{}) (*http.Response, error) {
	var req *http.Request
	var resp *http.Response
	var reqLogger *logrus.Entry
	var err error

	// We lock here so that we only handle a single request at a time,
	// making it simpler to handle Too Many Requests errors.
	rc.mu.Lock()
	defer rc.mu.Unlock()

	for {
		req, err = reqFunc()
		if err != nil {
			return nil, err
		}
		resp, err = rc.client.Do(req)
		if err != nil {
			return nil, err
		}
		reqLogger = rc.logger.WithFields(logrus.Fields{
			"method": req.Method,
			"url":    req.URL,
		})

		// Check if we got a Too Many Requests response, if we did
		// wait until retryAfter and try the same request again
		retryAfter := ExtractRetryAfter(resp)
		if retryAfter <= 0 {
			break
		}
		if err := resp.Body.Close(); err != nil {
			return nil, err
		}
		reqLogger.WithField("retryAfter", retryAfter).Info("Got a 429 response, limiting")
		time.Sleep(retryAfter)
	}

	defer func() {
		if cerr := resp.Body.Close(); err == nil {
			err = cerr
		}
	}()

	err = CheckResponse(resp)
	if err != nil {
		reqLogger.WithField("error", err).Warn("Bad response")
		return nil, err
	}

	if v != nil {
		err = json.NewDecoder(resp.Body).Decode(v)
		if err != nil {
			reqLogger.WithField("error", err).Error("Could not decode response as JSON")
			return nil, err
		}
	}

	reqLogger.Debug("Request was successful")
	return resp, nil
}

// Attempts to extract the Retry-After header value from a Too Many Requests (429)
// response. Returns 0 if the response is not a 429 response. Otherwise returns a
// duration matching the Retry-After header if present, or a default duration if
// it could not be extracted.
func ExtractRetryAfter(resp *http.Response) time.Duration {
	if resp.StatusCode != http.StatusTooManyRequests {
		return 0
	}
	retryAfter, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil {
		return defaultRetryAfter
	}
	return time.Duration(retryAfter) * time.Millisecond
}

// Takes a response and returns an error if the status code is not within
// the 200-299 range.
func CheckResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return nil
	}
	return createErrorResponse(resp)
}

func (rc *RestClient) GetGateway(ctx context.Context) (*Gateway, error) {
	reqFunc := rc.NewRequestCreatorFunc(ctx, "GET", "gateway", nil)
	gateway := &Gateway{}
	_, err := rc.Do(reqFunc, gateway)
	return gateway, err
}

// https://discordapp.com/developers/docs/resources/channel#create-message
func (rc *RestClient) CreateMessage(ctx context.Context, channelId string, content string) error {
	path := fmt.Sprintf("channels/%s/messages", channelId)
	body := struct {
		Content string `json:"content"`
	}{Content: content}
	reqFunc := rc.NewRequestCreatorFunc(ctx, "POST", path, body)
	_, err := rc.Do(reqFunc, nil)
	return err
}
