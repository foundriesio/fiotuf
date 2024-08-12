package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type httpRes struct {
	StatusCode int
	Body       []byte
	Header     http.Header
}

func (res httpRes) Json(data interface{}) error {
	return json.Unmarshal(res.Body, data)
}

func (res httpRes) String() string {
	return string(res.Body)
}

func readResponse(r *http.Response) (*httpRes, error) {
	defer r.Body.Close()
	res := &httpRes{
		StatusCode: r.StatusCode,
		Header:     r.Header,
	}
	var err error
	res.Body, err = io.ReadAll(r.Body)
	if err != nil {
		return res, fmt.Errorf("Unable to read response from %s: %w", r.Request.URL, err)
	}
	return res, nil
}

func httpDoOnce(client *http.Client, method, url string, headers map[string]string, data interface{}) (*httpRes, error) {
	var dataBytes []byte
	if data != nil {
		var err error
		dataBytes, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, url, bytes.NewBuffer(dataBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Add("User-Agent", "fioconfig-client/2")
	req.Header.Add("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Add(k, v)
	}
	req.Close = true

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Unable to %s: %s - %w", method, url, err)
	}
	return readResponse(res)
}

func httpDo(client *http.Client, method, url string, headers map[string]string, data interface{}) (*httpRes, error) {
	var err error
	var res *httpRes
	for _, delay := range []int{0, 1, 2, 5, 13, 30} {
		if delay != 0 {
			log.Printf("HTTP %s to %s failed, trying again in %d seconds", url, method, delay)
			time.Sleep(time.Second * time.Duration(delay))
		}
		res, err = httpDoOnce(client, method, url, headers, data)
		if err == nil && res.StatusCode != 0 && res.StatusCode < 500 {
			break
		}
	}
	return res, err
}

func httpGet(client *http.Client, url string, headers map[string]string) (*httpRes, error) {
	return httpDo(client, http.MethodGet, url, headers, nil)
}
