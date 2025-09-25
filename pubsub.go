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

	var pixels []Pixel
	var room = "default"

	// Parse binary data
	if len(data) >= 4 {
		// Read pixel count (first 4 bytes, little-endian)
		pixelCount := int(uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24)
		fmt.Printf("[DEBUG] onPixelUpdate received binary data with %d pixels\n", pixelCount)
		
		pixels = make([]Pixel, 0, pixelCount)
		offset := 4
		
		for i := 0; i < pixelCount && offset+8 <= len(data); i++ {
			// Read x (2 bytes, little-endian)
			x := int(uint16(data[offset]) | uint16(data[offset+1])<<8)
			offset += 2
			
			// Read y (2 bytes, little-endian)
			y := int(uint16(data[offset]) | uint16(data[offset+1])<<8)
			offset += 2
			
			// Read color (4 bytes, little-endian)
			colorValue := uint32(data[offset]) | uint32(data[offset+1])<<8 | uint32(data[offset+2])<<16 | uint32(data[offset+3])<<24
			offset += 4
			
			// Convert to hex color string
			color := fmt.Sprintf("#%06x", colorValue&0xFFFFFF)
			
			pixels = append(pixels, Pixel{
				X:        x,
				Y:        y,
				Color:    color,
				UserID:   "unknown", // Not included in binary format
				Username: "unknown", // Not included in binary format
			})
		}
	} else {
		fmt.Printf("[ERROR] onPixelUpdate insufficient binary data: %d bytes\n", len(data))
		return 1
	}

	fmt.Printf("[DEBUG] onPixelUpdate processing %d pixels for room %s\n", len(pixels), room)

	// Get pooled database connection
	db, dbErr := getCanvasDB()
	if dbErr != 0 {
		fmt.Printf("[ERROR] onPixelUpdate database connection failed\n")
		return 1
	}

	// Batch process pixels for better performance
	validPixels := make([]Pixel, 0, len(pixels))
	for _, pixel := range pixels {
		// Validate coordinates before processing
		if pixel.X >= 0 && pixel.X < CanvasWidth && pixel.Y >= 0 && pixel.Y < CanvasHeight {
			validPixels = append(validPixels, pixel)
		}
	}
	fmt.Printf("[DEBUG] onPixelUpdate processing %d valid pixels\n", len(validPixels))

	// Batch save all valid pixels
	successCount := 0
	for _, pixel := range validPixels {
		pixelData, err := json.Marshal(pixel)
		if err != nil {
			fmt.Printf("[ERROR] Failed to marshal pixel (%d,%d): %v\n", pixel.X, pixel.Y, err)
			continue
		}
		
		key := fmt.Sprintf("/%s/%d:%d", room, pixel.X, pixel.Y)
		err = db.Put(key, pixelData)
		if err != nil {
			fmt.Printf("[ERROR] Failed to save pixel (%d,%d) to database: %v\n", pixel.X, pixel.Y, err)
		} else {
			successCount++
			fmt.Printf("[DEBUG] Successfully saved pixel (%d,%d) to key: %s\n", pixel.X, pixel.Y, key)
		}
	}
	
	fmt.Printf("[DEBUG] onPixelUpdate saved %d/%d pixels successfully\n", successCount, len(validPixels))

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

	var chatMessage ChatMessage
	room := "default"

	// Parse binary data
	offset := 0
	if len(data) < 4 {
		fmt.Printf("[ERROR] onChatMessages insufficient binary data: %d bytes\n", len(data))
		return 1
	}

	// Read messageId length and content
	messageIdLength := int(uint32(data[offset]) | uint32(data[offset+1])<<8 | uint32(data[offset+2])<<16 | uint32(data[offset+3])<<24)
	offset += 4
	if offset+messageIdLength > len(data) {
		fmt.Printf("[ERROR] onChatMessages invalid messageId length: %d\n", messageIdLength)
		return 1
	}
	chatMessage.ID = string(data[offset : offset+messageIdLength])
	offset += messageIdLength

	// Read userId length and content
	if offset+4 > len(data) {
		fmt.Printf("[ERROR] onChatMessages insufficient data for userId length\n")
		return 1
	}
	userIdLength := int(uint32(data[offset]) | uint32(data[offset+1])<<8 | uint32(data[offset+2])<<16 | uint32(data[offset+3])<<24)
	offset += 4
	if offset+userIdLength > len(data) {
		fmt.Printf("[ERROR] onChatMessages invalid userId length: %d\n", userIdLength)
		return 1
	}
	chatMessage.UserID = string(data[offset : offset+userIdLength])
	offset += userIdLength

	// Read username length and content
	if offset+4 > len(data) {
		fmt.Printf("[ERROR] onChatMessages insufficient data for username length\n")
		return 1
	}
	usernameLength := int(uint32(data[offset]) | uint32(data[offset+1])<<8 | uint32(data[offset+2])<<16 | uint32(data[offset+3])<<24)
	offset += 4
	if offset+usernameLength > len(data) {
		fmt.Printf("[ERROR] onChatMessages invalid username length: %d\n", usernameLength)
		return 1
	}
	chatMessage.Username = string(data[offset : offset+usernameLength])
	offset += usernameLength

	// Read message length and content
	if offset+4 > len(data) {
		fmt.Printf("[ERROR] onChatMessages insufficient data for message length\n")
		return 1
	}
	messageLength := int(uint32(data[offset]) | uint32(data[offset+1])<<8 | uint32(data[offset+2])<<16 | uint32(data[offset+3])<<24)
	offset += 4
	if offset+messageLength > len(data) {
		fmt.Printf("[ERROR] onChatMessages invalid message length: %d\n", messageLength)
		return 1
	}
	chatMessage.Message = string(data[offset : offset+messageLength])
	offset += messageLength

	// Read timestamp
	if offset+4 > len(data) {
		fmt.Printf("[ERROR] onChatMessages insufficient data for timestamp\n")
		return 1
	}
	chatMessage.Timestamp = int64(uint32(data[offset]) | uint32(data[offset+1])<<8 | uint32(data[offset+2])<<16 | uint32(data[offset+3])<<24)

	fmt.Printf("[DEBUG] onChatMessages received binary message: %s from %s\n", chatMessage.ID, chatMessage.Username)

	// Get pooled database connection
	db, dbErr := getChatDB()
	if dbErr != 0 {
		fmt.Printf("[ERROR] onChatMessages database connection failed: %d\n", dbErr)
		return 1
	}

	messageData, err := json.Marshal(chatMessage)
	if err != nil {
		fmt.Printf("[ERROR] onChatMessages failed to marshal message %s: %v\n", chatMessage.ID, err)
		return 1
	}

	key := fmt.Sprintf("/%s/%s", room, chatMessage.ID)
	err = db.Put(key, messageData)
	if err != nil {
		fmt.Printf("[ERROR] onChatMessages failed to save message %s to database: %v\n", chatMessage.ID, err)
		return 1
	}

	fmt.Printf("[DEBUG] onChatMessages successfully saved message %s to key: %s\n", chatMessage.ID, key)

	return 0
}

