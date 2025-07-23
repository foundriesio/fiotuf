package register

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DEVICE_API = "https://api.foundries.io/ota/devices/"
	OAUTH_API  = "https://app.foundries.io/oauth"
)

type HttpHeaders map[string]string

func dumpRespError(message string, code int, resp map[string]interface{}) {
	fmt.Fprintf(os.Stderr, "%s: HTTP_%d\n", message, code)
	if data, ok := resp["data"].(string); ok && len(data) > 0 {
		fmt.Fprintln(os.Stderr, data)
	}
	for k, v := range resp {
		fmt.Fprintf(os.Stderr, "%s: %v\n", k, v)
	}
}

// Returns the access_token on success, and empty string on errors
func getOauthToken(factory, deviceUUID string) string {
	env := os.Getenv(ENV_OAUTH_BASE)
	wheels := []rune{'|', '/', '-', '\\'}
	headers := map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}
	url := OAUTH_API
	if env != "" {
		url = env
	}

	data := fmt.Sprintf("client_id=%s&scope=%s:devices:create", deviceUUID, factory)
	resp, code, err := httpPost(url+"/authorization/device/", headers, data)
	if err != nil || code != 200 {
		dumpRespError("Unable to create device authorization request", code, resp)
		return ""
	}

	fmt.Println("\n----------------------------------------------------------------------------")
	fmt.Println("Visit the link below in your browser to authorize this new device. This link")
	fmt.Printf("will expire in %d minutes.\n", int(resp["expires_in"].(float64))/60)
	fmt.Printf("  Device Name: %s\n", deviceUUID)
	fmt.Printf("  User code: %s\n", resp["user_code"])
	fmt.Printf("  Browser URL: %s\n\n", resp["verification_uri"])

	data = fmt.Sprintf(
		"grant_type=urn:ietf:params:oauth:grant-type:device_code&device_code=%s&client_id=%s&scope=%s:devices:create",
		resp["device_code"], deviceUUID, factory,
	)
	interval := int(resp["interval"].(float64))
	i := 0

	fmt.Println("oauth data=", data)
	for {
		tokenResp, code, err := httpPost(url+"/token/", headers, data)
		if err == nil && code == 200 {
			return tokenResp["access_token"].(string)
		}
		if code != 400 {
			fmt.Printf("HTTP(%d) error...\n", code)
			time.Sleep(2 * time.Second)
			continue
		}
		if tokenResp["error"] == "authorization_pending" {
			fmt.Printf("Waiting for authorization %c\r", wheels[i%len(wheels)])
			i++
			time.Sleep(time.Duration(interval) * time.Second)
		} else {
			dumpRespError("Error authorizing device", code, tokenResp)
			return ""
		}
	}
}

func httpPost(url string, headers map[string]string, data string) (map[string]interface{}, int, error) {
	req, err := http.NewRequest("POST", url, strings.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var jsonResp map[string]interface{}
	json.Unmarshal(body, &jsonResp)
	return jsonResp, resp.StatusCode, nil
}

// Headers of a request to the device registration endpoint DEVICE_API
func AuthGetHttpHeaders(opt LmpOptions) (HttpHeaders, error) {
	headers := map[string]string{"Content-type": "application/json"}
	if opt.ApiToken != "" {
		headers[opt.ApiTokenHeader] = opt.ApiToken
		return headers, nil
	}
	fmt.Println("Foundries providing auth token")
	token := getOauthToken(opt.Factory, opt.UUID)
	if token == "" {
		return nil, fmt.Errorf("failed to get OAuth token for factory %s and device %s", opt.Factory, opt.UUID)
	}
	tokenBase64 := base64.StdEncoding.EncodeToString([]byte(token))
	headers["Authorization"] = "Bearer " + tokenBase64
	return headers, nil
}

// Register device using the oauth token. Token need "devices:create" scope
func AuthRegisterDevice(headers HttpHeaders, device map[string]interface{}) (map[string]interface{}, error) {
	api := os.Getenv(ENV_DEVICE_API)
	if api == "" {
		api = DEVICE_API
	}
	data, _ := json.MarshalIndent(device, "", "  ")

	fmt.Printf("Registering device %s with API: %s\n", device["name"], api)
	fmt.Println("Headers:", headers)
	fmt.Println("Data:", string(data))
	jsonResp, code, err := httpPost(api, headers, string(data))
	if code != 201 || err != nil {
		dumpRespError("Unable to create device", code, jsonResp)
		return nil, fmt.Errorf("unable to create device %w %d %s", err, code, jsonResp)
	}
	return jsonResp, nil
}

func AuthPingServer() error {
	api := os.Getenv(ENV_DEVICE_API)
	if api == "" {
		api = DEVICE_API
	}
	fmt.Printf("Using DEVICE_API: %s\n", api)
	resp, err := http.Get(api)
	if err != nil || resp.StatusCode > 500 {
		fmt.Fprintln(os.Stderr, "Ping failed")
		return fmt.Errorf("ping failed %w", err)
	}
	return nil
}
