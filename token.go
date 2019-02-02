/*
 * Copyright 2018 AccelByte Inc
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package iam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/dgrijalva/jwt-go"
)

func (client *DefaultClient) validateAccessToken(accessToken string) (bool, error) {
	form := url.Values{}
	form.Add("token", accessToken)
	req, err := http.NewRequest(http.MethodPost, client.config.BaseURL+verifyPath, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return false, fmt.Errorf("unable to create new http request %v", err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(client.config.ClientID, client.config.ClientSecret)
	httpClient := http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("unable to do http request %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	return true, nil
}

func (client *DefaultClient) validateJWT(token string) (*JWTClaims, error) {
	if token == "" {
		return nil, fmt.Errorf("token is empty")
	}

	var jwtClaims = JWTClaims{}
	keyFunction := func(token *jwt.Token) (interface{}, error) {
		keyID, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("token does not have ID")
		}
		publicKey, err := client.getPublicKey(keyID)
		if err != nil {
			return nil, err
		}
		return publicKey, nil
	}

	_, err := jwt.ParseWithClaims(token, &jwtClaims, keyFunction)
	if err != nil {
		return nil, err
	}

	return &jwtClaims, nil
}

func (client *DefaultClient) tokenRevoked(token string) bool {
	return client.revocationFilter.MightContain([]byte(token))
}

func (client *DefaultClient) userRevoked(userID string, issuedAt int64) bool {
	revokedAt := client.revokedUsers[userID]
	return revokedAt.Unix() >= issuedAt
}

func (client *DefaultClient) refreshAccessToken() {
	var tokenRefreshInterval time.Duration
	backOffTime := time.Second
	for {
		tokenRefreshInterval, client.tokenRefreshError = client.clientTokenGrant()
		if client.tokenRefreshError != nil {
			time.Sleep(backOffTime)
			if backOffTime < maxBackOffTime {
				backOffTime *= 2
			}
			continue
		}
		backOffTime = time.Second
		time.Sleep(tokenRefreshInterval)
	}
}

func (client *DefaultClient) clientTokenGrant() (time.Duration, error) {
	form := url.Values{}
	form.Add("grant_type", "client_credentials")
	req, err := http.NewRequest(http.MethodPost, client.config.BaseURL+grantPath, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(client.config.ClientID, client.config.ClientSecret)
	httpClient := http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var tokenResponse *TokenResponse
	err = json.Unmarshal(bodyBytes, &tokenResponse)
	if err != nil {
		return 0, fmt.Errorf("unable to unmarshal response body: %v", err)
	}

	client.clientAccessToken = tokenResponse.AccessToken
	refreshInterval := time.Duration(float64(tokenResponse.ExpiresIn)*defaultTokenRefreshRate) * time.Second
	return refreshInterval, nil
}