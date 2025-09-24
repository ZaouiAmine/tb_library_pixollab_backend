package lib

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/taubyte/go-sdk/event"
)

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

