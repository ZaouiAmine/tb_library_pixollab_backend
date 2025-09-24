package lib

import (
	"fmt"
	"sync"

	"github.com/taubyte/go-sdk/database"
)

// Database connection pool for performance optimization
var (
	canvasDB database.Database
	chatDB   database.Database
	dbMutex  sync.RWMutex
	dbInit   bool
)

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
		fmt.Printf("[DEBUG] Database already initialized\n")
		return 0 // Already initialized
	}

	fmt.Printf("[DEBUG] Initializing database connections\n")
	var err error
	canvasDB, err = database.New("/canvas")
	if err != nil {
		fmt.Printf("[ERROR] Failed to create canvas database: %v\n", err)
		return 1
	}
	fmt.Printf("[DEBUG] Canvas database connection created\n")

	chatDB, err = database.New("/chat")
	if err != nil {
		fmt.Printf("[ERROR] Failed to create chat database: %v\n", err)
		return 1
	}
	fmt.Printf("[DEBUG] Chat database connection created\n")

	dbInit = true
	fmt.Printf("[DEBUG] Database initialization completed\n")
	return 0
}

// Get canvas database connection
func getCanvasDB() (database.Database, uint32) {
	if !dbInit {
		if initDatabases() != 0 {
			var emptyDB database.Database
			return emptyDB, 1
		}
	}
	return canvasDB, 0
}

// Get chat database connection
func getChatDB() (database.Database, uint32) {
	if !dbInit {
		if initDatabases() != 0 {
			var emptyDB database.Database
			return emptyDB, 1
		}
	}
	return chatDB, 0
}

