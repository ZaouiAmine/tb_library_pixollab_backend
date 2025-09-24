package lib

import (
	"encoding/json"
	"fmt"

	http "github.com/taubyte/go-sdk/http/event"
)

func setCORSHeaders(h http.Event) {
	h.Headers().Set("Access-Control-Allow-Origin", "*")
	h.Headers().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	h.Headers().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func handleHTTPError(h http.Event, err error, code int) uint32 {
	h.Write([]byte(err.Error()))
	h.Return(code)
	return 1
}

func getRoomParam(h http.Event) string {
	room, err := h.Query().Get("room")
	if err != nil {
		return "default"
	}
	return room
}

func getRoomParamRequired(h http.Event) (string, uint32) {
	room, err := h.Query().Get("room")
	if err != nil {
		h.Write([]byte(err.Error()))
		h.Return(400)
		return "", 1
	}
	return room, 0
}

func sendJSONResponse(h http.Event, data interface{}) uint32 {
	fmt.Printf("[DEBUG] sendJSONResponse called with data type: %T\n", data)
	jsonData, err := json.Marshal(data)
	if err != nil {
		fmt.Printf("[ERROR] sendJSONResponse JSON marshal error: %v\n", err)
		h.Write([]byte("{\"error\":\"Failed to marshal JSON\"}"))
		h.Return(500)
		return 1
	}
	fmt.Printf("[DEBUG] sendJSONResponse marshaled %d bytes of JSON data\n", len(jsonData))
	h.Headers().Set("Content-Type", "application/json")
	h.Write(jsonData)
	fmt.Printf("[DEBUG] sendJSONResponse wrote JSON data successfully\n")
	h.Return(200)
	return 0
}

