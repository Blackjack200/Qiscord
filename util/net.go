package util

import (
	"io"
	"net/http"
)

func UrlGet(url string) (io.ReadCloser, error) {
	retry := 255
	var lastErr error
	for retry > 0 {
		req, err := http.Get(url)
		if err != nil {
			lastErr = err
			retry--
			continue
		}
		return req.Body, err
	}
	return nil, lastErr
}
