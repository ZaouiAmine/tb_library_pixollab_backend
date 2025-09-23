package lib

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/taubyte/go-sdk/database"
	"github.com/taubyte/go-sdk/event"
	http "github.com/taubyte/go-sdk/http/event"
	pubsub "github.com/taubyte/go-sdk/pubsub/node"
)

type Pixel struct {
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Color    string `json:"color"`
	UserID   string `json:"userId"`
	Username string `json:"username"`
}

type ChatMessage struct {
	ID        string `json:"messageId"`
	UserID    string `json:"userId"`
	Username  string `json:"username"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

const CanvasWidth = 90
const CanvasHeight = 90

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

func openDatabase(path string) (database.Database, uint32) {
	db, err := database.New(path)
	if err != nil {
		return db, 1
	}
	return db, 0
}

func sendJSONResponse(h http.Event, data interface{}) uint32 {
	jsonData, err := json.Marshal(data)
	if err != nil {
		h.Write([]byte("{\"error\":\"Failed to marshal JSON\"}"))
		h.Return(500)
		return 1
	}

	// Set headers before writing
	h.Headers().Set("Content-Type", "application/json")
	h.Headers().Set("Access-Control-Allow-Origin", "*")

	// Write the JSON data
	if len(jsonData) > 0 {
		h.Write(jsonData)
	} else {
		h.Write([]byte("[]"))
	}

	h.Return(200)
	return 0
}

//export getChannelURL
func getChannelURL(e event.Event) uint32 {
	h, err := e.HTTP()
	if err != nil {
		return 1
	}
	setCORSHeaders(h)
	channelName, err := h.Query().Get("channel")
	if err != nil {
		h.Write([]byte("channel parameter required"))
		h.Return(400)
		return 1
	}
	channel, err := pubsub.Channel(channelName)
	if err != nil {
		h.Write([]byte(err.Error()))
		h.Return(500)
		return 1
	}
	channel.Subscribe()
	url, err := channel.WebSocket().Url()
	if err != nil {
		h.Write([]byte(err.Error()))
		h.Return(500)
		return 1
	}
	h.Headers().Set("Content-Type", "text/plain")
	h.Write([]byte(url.Path))
	h.Return(200)
	return 0
}

//export getCanvas
func getCanvas(e event.Event) uint32 {
	h, err := e.HTTP()
	if err != nil {
		return 1
	}
	setCORSHeaders(h)
	room, code := getRoomParamRequired(h)
	if code != 0 {
		return code
	}

	// Initialize canvas with default white pixels
	canvas := make([][]string, CanvasHeight)
	for y := range canvas {
		canvas[y] = make([]string, CanvasWidth)
		for x := range canvas[y] {
			canvas[y][x] = "#ffffff"
		}
	}

	// Try to open database, but don't fail if it doesn't exist
	db, err := database.New("/canvas")
	if err != nil {
		// If database doesn't exist or can't be opened, return empty canvas
		return sendJSONResponse(h, canvas)
	}

	// Get all keys for this room
	roomPrefix := fmt.Sprintf("/%s/", room)
	keys, err := db.List(roomPrefix)
	if err != nil {
		// If we can't list keys, return empty canvas
		return sendJSONResponse(h, canvas)
	}

	// Process each pixel
	for _, key := range keys {
		// Ensure the key is longer than the room prefix
		if len(key) <= len(roomPrefix) {
			continue
		}

		// Extract coordinates from key
		coordPart := key[len(roomPrefix):]
		var x, y int
		if n, err := fmt.Sscanf(coordPart, "%d:%d", &x, &y); n == 2 && err == nil {
			// Validate coordinates are within bounds
			if x >= 0 && x < CanvasWidth && y >= 0 && y < CanvasHeight {
				// Get pixel data
				pixelData, err := db.Get(key)
				if err == nil && len(pixelData) > 0 {
					var pixel Pixel
					if json.Unmarshal(pixelData, &pixel) == nil {
						canvas[y][x] = pixel.Color
					}
				}
			}
		}
	}

	return sendJSONResponse(h, canvas)
}

//export clearData
func clearData(e event.Event) uint32 {
	h, err := e.HTTP()
	if err != nil {
		return 1
	}
	setCORSHeaders(h)
	room := getRoomParam(h)
	dataType, err := h.Query().Get("type")
	if err != nil {
		h.Write([]byte("type parameter required (canvas or chat)"))
		h.Return(400)
		return 1
	}
	var dbPath, successMsg string
	switch dataType {
	case "canvas":
		dbPath = "/canvas"
		successMsg = "Canvas cleared"
	case "chat":
		dbPath = "/chat"
		successMsg = "Chat cleared"
	default:
		h.Write([]byte("type must be 'canvas' or 'chat'"))
		h.Return(400)
		return 1
	}
	db, err := database.New(dbPath)
	if err != nil {
		h.Write([]byte(err.Error()))
		h.Return(500)
		return 1
	}
	keys, err := db.List(fmt.Sprintf("/%s/", room))
	if err == nil && len(keys) > 0 {
		for _, key := range keys {
			db.Delete(key)
		}
	}
	h.Write([]byte(successMsg))
	h.Return(200)
	return 0
}

//export getMessages
func getMessages(e event.Event) uint32 {
	h, err := e.HTTP()
	if err != nil {
		return 1
	}
	setCORSHeaders(h)
	room, code := getRoomParamRequired(h)
	if code != 0 {
		return code
	}

	var messages []ChatMessage

	// Try to open database, but don't fail if it doesn't exist
	db, err := database.New("/chat")
	if err != nil {
		// If database doesn't exist, return empty messages
		return sendJSONResponse(h, messages)
	}

	// Get all keys for this room
	roomPrefix := fmt.Sprintf("/%s/", room)
	keys, err := db.List(roomPrefix)
	if err != nil {
		// If we can't list keys, return empty messages
		return sendJSONResponse(h, messages)
	}

	// Process each message
	for _, key := range keys {
		// Ensure the key is longer than the room prefix
		if len(key) <= len(roomPrefix) {
			continue
		}

		messageData, err := db.Get(key)
		if err == nil && len(messageData) > 0 {
			var message ChatMessage
			if json.Unmarshal(messageData, &message) == nil {
				messages = append(messages, message)
			}
		}
	}

	// Sort messages by timestamp
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp < messages[j].Timestamp
	})

	return sendJSONResponse(h, messages)
}

//export onPixelUpdate
func onPixelUpdate(e event.Event) uint32 {
	channel, err := e.PubSub()
	if err != nil {
		return 1
	}
	data, err := channel.Data()
	if err != nil {
		return 1
	}
	var pixelBatch struct {
		Pixels    []Pixel `json:"pixels"`
		Room      string  `json:"room"`
		Timestamp int64   `json:"timestamp"`
		BatchId   string  `json:"batchId"`
		SourceId  string  `json:"sourceId"`
	}
	err = json.Unmarshal(data, &pixelBatch)
	if err != nil {
		return 1
	}
	room := pixelBatch.Room
	if room == "" {
		room = "default"
	}

	db, err := database.New("/canvas")
	if err != nil {
		return 1
	}

	// Process each pixel with bounds checking
	for _, pixel := range pixelBatch.Pixels {
		// Validate pixel coordinates are within canvas bounds
		if pixel.X >= 0 && pixel.X < CanvasWidth && pixel.Y >= 0 && pixel.Y < CanvasHeight {
			pixelData, err := json.Marshal(pixel)
			if err == nil {
				key := fmt.Sprintf("/%s/%d:%d", room, pixel.X, pixel.Y)
				db.Put(key, pixelData)
			}
		}
	}
	return 0
}

//export onChatMessages
func onChatMessages(e event.Event) uint32 {
	channel, err := e.PubSub()
	if err != nil {
		return 1
	}
	data, err := channel.Data()
	if err != nil {
		return 1
	}
	var message struct {
		ChatMessage
		Room     string `json:"room"`
		SourceId string `json:"sourceId"`
	}
	err = json.Unmarshal(data, &message)
	if err != nil {
		return 1
	}
	room := message.Room
	if room == "" {
		room = "default"
	}
	db, err := database.New("/chat")
	if err != nil {
		return 1
	}
	messageData, err := json.Marshal(message.ChatMessage)
	if err == nil {
		db.Put(fmt.Sprintf("/%s/%s", room, message.ID), messageData)
	}
	return 0
}
