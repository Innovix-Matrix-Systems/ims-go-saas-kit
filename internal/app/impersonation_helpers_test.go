package app_test

import (
	"encoding/json"
	"strings"
)

func containsCode(body, code string) bool {
	return strings.Contains(body, `"`+code+`"`)
}

// stripTrace reduces a problem body to what a client can compare: the code,
// title and detail (request-specific members dropped).
func stripTrace(body string) string {
	var problem struct {
		Code   string `json:"error_code"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &problem); err != nil {
		return body
	}
	encoded, _ := json.Marshal(problem)
	return string(encoded)
}
