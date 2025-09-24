package lib

import (
	"encoding/json"
	"fmt"

	"github.com/taubyte/go-sdk/database"
	"github.com/taubyte/go-sdk/event"
)

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

