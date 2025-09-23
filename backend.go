package lib

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/taubyte/go-sdk/database"
	"github.com/taubyte/go-sdk/event"
	http "github.com/taubyte/go-sdk/http/event"
	pubsub "github.com/taubyte/go-sdk/pubsub/node"
)

// Database connection pool for performance optimization
var (
	canvasDB database.Database
	chatDB   database.Database
	dbMutex  sync.RWMutex
	dbInit   bool
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

// Initialize database connections with pooling
func initDatabases() uint32 {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if dbInit {
		fmt.Printf("[LOG-DB-001] Database already initialized\n")
		return 0 // Already initialized
	}

	fmt.Printf("[LOG-DB-002] Initializing database connections\n")
	var err error
	canvasDB, err = database.New("/canvas")
	if err != nil {
		fmt.Printf("[ERROR-DB-003] Canvas database creation failed: %v\n", err)
		return 1
	}
	fmt.Printf("[LOG-DB-004] Canvas database created\n")

	chatDB, err = database.New("/chat")
	if err != nil {
		fmt.Printf("[ERROR-DB-005] Chat database creation failed: %v\n", err)
		return 1
	}
	fmt.Printf("[LOG-DB-006] Chat database created\n")

	dbInit = true
	fmt.Printf("[LOG-DB-007] Database initialization completed\n")
	return 0
}

// Get canvas database connection
func getCanvasDB() (database.Database, uint32) {
	if !dbInit {
		fmt.Printf("[LOG-DB-008] Canvas DB not initialized, initializing\n")
		if initDatabases() != 0 {
			fmt.Printf("[ERROR-DB-009] Canvas DB initialization failed\n")
			var emptyDB database.Database
			return emptyDB, 1
		}
	}
	fmt.Printf("[LOG-DB-010] Canvas DB connection retrieved\n")
	return canvasDB, 0
}

// Get chat database connection
func getChatDB() (database.Database, uint32) {
	if !dbInit {
		fmt.Printf("[LOG-DB-011] Chat DB not initialized, initializing\n")
		if initDatabases() != 0 {
			fmt.Printf("[ERROR-DB-012] Chat DB initialization failed\n")
			var emptyDB database.Database
			return emptyDB, 1
		}
	}
	fmt.Printf("[LOG-DB-013] Chat DB connection retrieved\n")
	return chatDB, 0
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
	fmt.Printf("[DEBUG] getCanvas called\n")
	h, err := e.HTTP()
	if err != nil {
		fmt.Printf("[ERROR] getCanvas HTTP error: %v\n", err)
		return 1
	}
	setCORSHeaders(h)
	room, code := getRoomParamRequired(h)
	if code != 0 {
		fmt.Printf("[ERROR] getCanvas room param error: %d\n", code)
		return code
	}
	fmt.Printf("[DEBUG] getCanvas room: %s\n", room)
	db, dbErr := getCanvasDB()
	if dbErr != 0 {
		return handleHTTPError(h, fmt.Errorf("database connection failed"), 500)
	}
	canvas := make([][]string, CanvasHeight)
	for y := range canvas {
		canvas[y] = make([]string, CanvasWidth)
		for x := range canvas[y] {
			canvas[y][x] = "#ffffff"
		}
	}
	keys, err := db.List(fmt.Sprintf("/%s/", room))
	fmt.Printf("[DEBUG] getCanvas found %d keys for room %s\n", len(keys), room)
	if err == nil {
		for _, key := range keys {
			if len(key) > len(fmt.Sprintf("/%s/", room)) {
				coordPart := key[len(fmt.Sprintf("/%s/", room)):]
				var x, y int
				if n, err := fmt.Sscanf(coordPart, "%d:%d", &x, &y); n == 2 && err == nil {
					fmt.Printf("[DEBUG] getCanvas processing pixel at (%d,%d)\n", x, y)
					// Validate coordinates before accessing canvas
					if x >= 0 && x < CanvasWidth && y >= 0 && y < CanvasHeight {
						pixelData, err := db.Get(key)
						if err == nil {
							var pixel Pixel
							if json.Unmarshal(pixelData, &pixel) == nil {
								canvas[y][x] = pixel.Color
								fmt.Printf("[DEBUG] getCanvas set pixel (%d,%d) to color %s\n", x, y, pixel.Color)
							} else {
								fmt.Printf("[ERROR] getCanvas failed to unmarshal pixel data for (%d,%d)\n", x, y)
							}
						} else {
							fmt.Printf("[ERROR] getCanvas failed to get pixel data for (%d,%d): %v\n", x, y, err)
						}
					} else {
						fmt.Printf("[ERROR] getCanvas invalid coordinates (%d,%d) - bounds: [0,%d) x [0,%d)\n", x, y, CanvasWidth, CanvasHeight)
					}
				} else {
					fmt.Printf("[ERROR] getCanvas failed to parse coordinates from key: %s\n", key)
				}
			}
		}
	} else {
		fmt.Printf("[ERROR] getCanvas failed to list keys: %v\n", err)
	}
	fmt.Printf("[DEBUG] getCanvas returning canvas data\n")
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
	fmt.Printf("[DEBUG] getMessages called\n")
	h, err := e.HTTP()
	if err != nil {
		fmt.Printf("[ERROR] getMessages HTTP error: %v\n", err)
		return 1
	}
	setCORSHeaders(h)
	room, code := getRoomParamRequired(h)
	if code != 0 {
		fmt.Printf("[ERROR] getMessages room param error: %d\n", code)
		return code
	}
	fmt.Printf("[DEBUG] getMessages room: %s\n", room)
	db, dbErr := getChatDB()
	if dbErr != 0 {
		return handleHTTPError(h, fmt.Errorf("database connection failed"), 500)
	}
	var messages []ChatMessage
	keys, err := db.List(fmt.Sprintf("/%s/", room))
	fmt.Printf("[DEBUG] getMessages found %d keys for room %s\n", len(keys), room)
	if err == nil {
		for _, key := range keys {
			if len(key) > len(fmt.Sprintf("/%s/", room)) {
				messageData, err := db.Get(key)
				if err == nil {
					var message ChatMessage
					if json.Unmarshal(messageData, &message) == nil {
						messages = append(messages, message)
						fmt.Printf("[DEBUG] getMessages loaded message %s from %s\n", message.ID, message.Username)
					} else {
						fmt.Printf("[ERROR] getMessages failed to unmarshal message data for key: %s\n", key)
					}
				} else {
					fmt.Printf("[ERROR] getMessages failed to get message data for key: %s, error: %v\n", key, err)
				}
			}
		}
	} else {
		fmt.Printf("[ERROR] getMessages failed to list keys: %v\n", err)
	}
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp < messages[j].Timestamp
	})
	fmt.Printf("[DEBUG] getMessages returning %d messages\n", len(messages))
	return sendJSONResponse(h, messages)
}

//export onPixelUpdate
func onPixelUpdate(e event.Event) uint32 {
	fmt.Printf("[LOG-PIXEL-001] onPixelUpdate called\n")
	channel, err := e.PubSub()
	if err != nil {
		fmt.Printf("[ERROR-PIXEL-002] PubSub error: %v\n", err)
		return 1
	}
	data, err := channel.Data()
	if err != nil {
		fmt.Printf("[ERROR-PIXEL-003] Channel data error: %v\n", err)
		return 1
	}
	fmt.Printf("[LOG-PIXEL-004] Received %d bytes of data\n", len(data))

	var pixelBatch struct {
		Pixels    []Pixel `json:"pixels"`
		Room      string  `json:"room"`
		Timestamp int64   `json:"timestamp"`
		BatchId   string  `json:"batchId"`
		SourceId  string  `json:"sourceId"`
	}

	// Try to unmarshal as compressed format first
	var compressedBatch struct {
		P [][]interface{} `json:"p"`
		R string          `json:"r"`
		T int64           `json:"t"`
		B string          `json:"b"`
		S string          `json:"s"`
	}

	var room string
	// Try compressed format first
	err = json.Unmarshal(data, &compressedBatch)
	if err == nil && len(compressedBatch.P) > 0 {
		// Handle compressed format
		fmt.Printf("[LOG-PIXEL-005] Compressed format with %d pixels\n", len(compressedBatch.P))
		room = compressedBatch.R
		if room == "" {
			room = "default"
		}

		// Convert compressed pixels to Pixel structs
		pixels := make([]Pixel, 0, len(compressedBatch.P))
		for _, p := range compressedBatch.P {
			if len(p) >= 3 {
				if x, ok := p[0].(float64); ok {
					if y, ok := p[1].(float64); ok {
						if color, ok := p[2].(string); ok {
							pixels = append(pixels, Pixel{
								X:        int(x),
								Y:        int(y),
								Color:    color,
								UserID:   compressedBatch.S,
								Username: "unknown",
							})
						}
					}
				}
			}
		}
		pixelBatch.Pixels = pixels
		pixelBatch.Room = room
		pixelBatch.Timestamp = compressedBatch.T
		pixelBatch.BatchId = compressedBatch.B
		pixelBatch.SourceId = compressedBatch.S
	} else {
		// Try uncompressed format
		err = json.Unmarshal(data, &pixelBatch)
		if err != nil {
			fmt.Printf("[ERROR-PIXEL-006] JSON unmarshal error: %v\n", err)
			return 1
		}
	}

	room = pixelBatch.Room
	if room == "" {
		room = "default"
	}
	fmt.Printf("[LOG-PIXEL-007] Processing %d pixels for room %s\n", len(pixelBatch.Pixels), room)

	// Get pooled database connection
	db, dbErr := getCanvasDB()
	if dbErr != 0 {
		fmt.Printf("[ERROR-PIXEL-008] Database connection failed\n")
		return 1
	}
	fmt.Printf("[LOG-PIXEL-009] Database connection successful\n")

	// Batch process pixels for better performance
	validPixels := make([]Pixel, 0, len(pixelBatch.Pixels))
	for _, pixel := range pixelBatch.Pixels {
		// Validate coordinates before processing
		if pixel.X >= 0 && pixel.X < CanvasWidth && pixel.Y >= 0 && pixel.Y < CanvasHeight {
			validPixels = append(validPixels, pixel)
		}
	}
	fmt.Printf("[LOG-PIXEL-010] Processing %d valid pixels\n", len(validPixels))

	// Batch save all valid pixels
	savedCount := 0
	failedCount := 0
	for _, pixel := range validPixels {
		pixelData, err := json.Marshal(pixel)
		if err == nil {
			key := fmt.Sprintf("/%s/%d:%d", room, pixel.X, pixel.Y)
			err = db.Put(key, pixelData)
			if err != nil {
				fmt.Printf("[ERROR-PIXEL-011] Save failed (%d,%d): %v\n", pixel.X, pixel.Y, err)
				failedCount++
			} else {
				fmt.Printf("[LOG-PIXEL-012] Saved (%d,%d) to %s\n", pixel.X, pixel.Y, key)
				savedCount++
			}
		} else {
			fmt.Printf("[ERROR-PIXEL-013] Marshal failed (%d,%d): %v\n", pixel.X, pixel.Y, err)
			failedCount++
		}
	}
	fmt.Printf("[LOG-PIXEL-014] Save complete: %d saved, %d failed\n", savedCount, failedCount)

	return 0
}

//export onChatMessages
func onChatMessages(e event.Event) uint32 {
	fmt.Printf("[LOG-CHAT-001] onChatMessages called\n")
	channel, err := e.PubSub()
	if err != nil {
		fmt.Printf("[ERROR-CHAT-002] PubSub error: %v\n", err)
		return 1
	}
	data, err := channel.Data()
	if err != nil {
		fmt.Printf("[ERROR-CHAT-003] Channel data error: %v\n", err)
		return 1
	}
	fmt.Printf("[LOG-CHAT-004] Received %d bytes of chat data\n", len(data))

	var message struct {
		ChatMessage
		Room     string `json:"room"`
		SourceId string `json:"sourceId"`
	}
	err = json.Unmarshal(data, &message)
	if err != nil {
		fmt.Printf("[ERROR-CHAT-005] JSON unmarshal error: %v\n", err)
		return 1
	}

	room := message.Room
	if room == "" {
		room = "default"
	}
	fmt.Printf("[LOG-CHAT-006] Processing message %s for room %s\n", message.ID, room)

	// Get pooled database connection
	db, dbErr := getChatDB()
	if dbErr != 0 {
		fmt.Printf("[ERROR-CHAT-007] Database connection failed\n")
		return 1
	}
	fmt.Printf("[LOG-CHAT-008] Database connection successful\n")

	messageData, err := json.Marshal(message.ChatMessage)
	if err == nil {
		key := fmt.Sprintf("/%s/%s", room, message.ID)
		err = db.Put(key, messageData)
		if err != nil {
			fmt.Printf("[ERROR-CHAT-009] Save failed for message %s: %v\n", message.ID, err)
		} else {
			fmt.Printf("[LOG-CHAT-010] Saved message %s to %s\n", message.ID, key)
		}
	} else {
		fmt.Printf("[ERROR-CHAT-011] Marshal failed for message %s: %v\n", message.ID, err)
	}

	return 0
}
