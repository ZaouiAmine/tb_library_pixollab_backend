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
	db, err := database.New("/canvas")
	if err != nil {
		fmt.Printf("[ERROR] getCanvas database error: %v\n", err)
		return handleHTTPError(h, err, 500)
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
	db, err := database.New("/chat")
	if err != nil {
		fmt.Printf("[ERROR] getMessages database error: %v\n", err)
		return handleHTTPError(h, err, 500)
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
	fmt.Printf("[DEBUG] onPixelUpdate called\n")
	channel, err := e.PubSub()
	if err != nil {
		fmt.Printf("[ERROR] onPixelUpdate PubSub error: %v\n", err)
		return 1
	}
	data, err := channel.Data()
	if err != nil {
		fmt.Printf("[ERROR] onPixelUpdate channel data error: %v\n", err)
		return 1
	}
	fmt.Printf("[DEBUG] onPixelUpdate received %d bytes of data\n", len(data))
	var pixelBatch struct {
		Pixels    []Pixel `json:"pixels"`
		Room      string  `json:"room"`
		Timestamp int64   `json:"timestamp"`
		BatchId   string  `json:"batchId"`
		SourceId  string  `json:"sourceId"`
	}
	err = json.Unmarshal(data, &pixelBatch)
	if err != nil {
		fmt.Printf("[ERROR] onPixelUpdate JSON unmarshal error: %v\n", err)
		return 1
	}
	room := pixelBatch.Room
	if room == "" {
		room = "default"
	}
	fmt.Printf("[DEBUG] onPixelUpdate processing %d pixels for room %s (batchId: %s, sourceId: %s)\n", 
		len(pixelBatch.Pixels), room, pixelBatch.BatchId, pixelBatch.SourceId)
	db, err := database.New("/canvas")
	if err != nil {
		fmt.Printf("[ERROR] onPixelUpdate database error: %v\n", err)
		return 1
	}
	for i, pixel := range pixelBatch.Pixels {
		fmt.Printf("[DEBUG] onPixelUpdate processing pixel %d: (%d,%d) color=%s\n", i, pixel.X, pixel.Y, pixel.Color)
		// Validate coordinates before processing
		if pixel.X >= 0 && pixel.X < CanvasWidth && pixel.Y >= 0 && pixel.Y < CanvasHeight {
			pixelData, err := json.Marshal(pixel)
			if err == nil {
				key := fmt.Sprintf("/%s/%d:%d", room, pixel.X, pixel.Y)
				err = db.Put(key, pixelData)
				if err == nil {
					fmt.Printf("[DEBUG] onPixelUpdate saved pixel (%d,%d) to key: %s\n", pixel.X, pixel.Y, key)
				} else {
					fmt.Printf("[ERROR] onPixelUpdate failed to save pixel (%d,%d): %v\n", pixel.X, pixel.Y, err)
				}
			} else {
				fmt.Printf("[ERROR] onPixelUpdate failed to marshal pixel (%d,%d): %v\n", pixel.X, pixel.Y, err)
			}
		} else {
			fmt.Printf("[ERROR] onPixelUpdate invalid coordinates (%d,%d) - bounds: [0,%d) x [0,%d)\n", 
				pixel.X, pixel.Y, CanvasWidth, CanvasHeight)
		}
	}
	fmt.Printf("[DEBUG] onPixelUpdate completed\n")
	return 0
}

//export onChatMessages
func onChatMessages(e event.Event) uint32 {
	fmt.Printf("[DEBUG] onChatMessages called\n")
	channel, err := e.PubSub()
	if err != nil {
		fmt.Printf("[ERROR] onChatMessages PubSub error: %v\n", err)
		return 1
	}
	data, err := channel.Data()
	if err != nil {
		fmt.Printf("[ERROR] onChatMessages channel data error: %v\n", err)
		return 1
	}
	fmt.Printf("[DEBUG] onChatMessages received %d bytes of data\n", len(data))
	var message struct {
		ChatMessage
		Room     string `json:"room"`
		SourceId string `json:"sourceId"`
	}
	err = json.Unmarshal(data, &message)
	if err != nil {
		fmt.Printf("[ERROR] onChatMessages JSON unmarshal error: %v\n", err)
		return 1
	}
	room := message.Room
	if room == "" {
		room = "default"
	}
	fmt.Printf("[DEBUG] onChatMessages processing message from %s in room %s (messageId: %s)\n", 
		message.Username, room, message.ID)
	db, err := database.New("/chat")
	if err != nil {
		fmt.Printf("[ERROR] onChatMessages database error: %v\n", err)
		return 1
	}
	messageData, err := json.Marshal(message.ChatMessage)
	if err == nil {
		key := fmt.Sprintf("/%s/%s", room, message.ID)
		err = db.Put(key, messageData)
		if err == nil {
			fmt.Printf("[DEBUG] onChatMessages saved message %s to key: %s\n", message.ID, key)
		} else {
			fmt.Printf("[ERROR] onChatMessages failed to save message %s: %v\n", message.ID, err)
		}
	} else {
		fmt.Printf("[ERROR] onChatMessages failed to marshal message %s: %v\n", message.ID, err)
	}
	fmt.Printf("[DEBUG] onChatMessages completed\n")
	return 0
}
