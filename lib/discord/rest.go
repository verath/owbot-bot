package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/Sirupsen/logrus"
	"net/http"
)

const (
	API_BASE_URL = "https://discordapp.com/api"
)

type RestClient struct {
	logger *logrus.Entry
	token  string
}

func NewRestClient(logger *logrus.Logger, token string) (*RestClient, error) {
	restLogger := logger.WithField("module", "discord-rest")

	return &RestClient{
		token:  token,
		logger: restLogger,
	}, nil
}

func (rc *RestClient) GetGateway() (*Gateway, error) {
	resp, err := http.Get(API_BASE_URL + "/gateway")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	gateway := Gateway{}
	err = json.NewDecoder(resp.Body).Decode(&gateway)
	if err != nil {
		return nil, err
	}
	return &gateway, nil
}

// https://discordapp.com/developers/docs/resources/channel#create-message
func (rc *RestClient) CreateMessage(channelId string, content string) error {
	url := API_BASE_URL + "/channels/" + channelId + "/messages"
	params := struct {
		Content string `json:"content"`
	}{content}
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.Header.Set("authorization", rc.token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// TODO: handle 429 - too many requests
		rc.logger.WithFields(logrus.Fields{
			"status": resp.StatusCode,
			"url":    url,
		}).Warn("Got a non-200 response from API")
		return errors.New("Non-200 response")
	}

	return nil
}
