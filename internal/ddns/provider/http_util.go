package ddnsprovider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func decodeJSONResponse(resp *http.Response, err error, out any) error {
	body, err := readResponseBody(resp, err)
	if err != nil {
		return err
	}
	if len(body) == 0 || out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func readResponseBody(resp *http.Response, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return body, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
