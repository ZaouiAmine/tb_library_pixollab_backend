package lib

import (
	"encoding/json"
	"fmt"

	"github.com/taubyte/go-sdk/event"
	pubsub "github.com/taubyte/go-sdk/pubsub/node"
)

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
		fmt.Printf("[DEBUG] onPixelUpdate received compressed format with %d pixels\n", len(compressedBatch.P))
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
			fmt.Printf("[ERROR] onPixelUpdate JSON unmarshal error: %v\n", err)
			return 1
		}
	}

	room = pixelBatch.Room
	if room == "" {
		room = "default"
	}
	fmt.Printf("[DEBUG] onPixelUpdate processing %d pixels for room %s\n", len(pixelBatch.Pixels), room)

	// Get pooled database connection
	db, dbErr := getCanvasDB()
	if dbErr != 0 {
		fmt.Printf("[ERROR] onPixelUpdate database connection failed\n")
		return 1
	}

	// Batch process pixels for better performance
	validPixels := make([]Pixel, 0, len(pixelBatch.Pixels))
	for _, pixel := range pixelBatch.Pixels {
		// Validate coordinates before processing
		if pixel.X >= 0 && pixel.X < CanvasWidth && pixel.Y >= 0 && pixel.Y < CanvasHeight {
			validPixels = append(validPixels, pixel)
		}
	}
	fmt.Printf("[DEBUG] onPixelUpdate processing %d valid pixels\n", len(validPixels))

	// Batch save all valid pixels
	for _, pixel := range validPixels {
		pixelData, err := json.Marshal(pixel)
		if err == nil {
			key := fmt.Sprintf("/%s/%d:%d", room, pixel.X, pixel.Y)
			err = db.Put(key, pixelData)
			if err != nil {
				fmt.Printf("[ERROR] Failed to save pixel (%d,%d) to database: %v\n", pixel.X, pixel.Y, err)
			} else {
				fmt.Printf("[DEBUG] Successfully saved pixel (%d,%d) to key: %s\n", pixel.X, pixel.Y, key)
			}
		} else {
			fmt.Printf("[ERROR] Failed to marshal pixel (%d,%d): %v\n", pixel.X, pixel.Y, err)
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

	// Get pooled database connection
	db, dbErr := getChatDB()
	if dbErr != 0 {
		return 1
	}

	messageData, err := json.Marshal(message.ChatMessage)
	if err == nil {
		key := fmt.Sprintf("/%s/%s", room, message.ID)
		db.Put(key, messageData) // Don't check error to avoid blocking
	}

	return 0
}

