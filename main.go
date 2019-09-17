package main

import (
	"fmt"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/docopt/docopt-go"
	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
)

var c Config

type ClientMessage struct {
	SubjectID string `json:"subjectId,omitempty"`
	Action    string `json:"action,omitempty"`
	Content   string `json:"content,omitempty"`
}

type ServerMessage struct {
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message,omitempty"`
}

var client *PsychTimer
var action = make(chan ClientMessage) // broadcast channel
var serverMsg = make(chan ServerMessage)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// MapServer does stuff
func MapServer(root map[string]string) http.Handler {
	return &mapHandler{root}
}

type mapHandler struct {
	root map[string]string
}

func (m *mapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upath := strings.TrimPrefix(r.URL.Path, "/")
	//	fmt.Printf("Trying to read %s\n", upath)
	//	fmt.Printf("have: %+v\n", m.root[upath])
	ctype := mime.TypeByExtension(filepath.Ext(upath))
	//	fmt.Printf("Detected content type: %+v\n", ctype)
	w.Header().Set("Content-Type", ctype)
	fmt.Fprint(w, m.root[upath])
}

func handler(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, "Hello World!")
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Upgrade initial GET request to a websocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	// Make sure we close the connection when the function returns
	defer ws.Close()

	fmt.Println("Upgraded to ws!")

	// Register our new client
	client = NewPsychTimer(c, ws)

	for {
		var msg ClientMessage
		// Read in a new message as JSON and map it to a Message object
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("error: %v", err)
			client = nil
			break
		}
		fmt.Printf("Received message %+v", msg)
		// Send the newly received message to the broadcast channel
		action <- msg
	}
}

func handleActions() {
	for {
		// Grab the next message from the broadcast channel
		msg := <-action

		if msg.Action == "START" {
			go client.RunOne(msg.SubjectID, serverMsg)

		}
		if msg.Action == "KEY" {
			fmt.Printf("Received keycode %s for subject %s\n", msg.Content, msg.SubjectID)
		}
	}
}

func handleServerMessages() {
	for {
		sMsg := <-serverMsg

		err := client.Conn.WriteJSON(sMsg)
		if err != nil {
			log.Printf("error: %v", err)
			client.Conn.Close()
		}
	}
}

func main() {
	usage := `Psych Timer
	
Usage:
	psych-timer <config>
	
`

	arguments, _ := docopt.ParseDoc(usage)
	fmt.Printf("%+v\n", arguments)

	viper.SetConfigName(arguments["<config>"].(string)) // name of config file (without extension)
	viper.AddConfigPath("$HOME/.psych_timer")           // call multiple times to add many search paths
	viper.AddConfigPath(".")                            // optionally look for config in the working directory
	err := viper.ReadInConfig()                         // Find and read the config file
	if err != nil {                                     // Handle errors reading the config file
		panic(fmt.Errorf("fatal error config file: %s", err))
	}

	err = viper.Unmarshal(&c)
	if err != nil {
		fmt.Errorf("unable to decode into struct, %v", err)
	}

	fmt.Printf("%+v\n", c)

	http.Handle("/static/", MapServer(Data))

	// Configure websocket route
	http.HandleFunc("/ws", handleConnections)

	go handleActions()
	go handleServerMessages()
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
