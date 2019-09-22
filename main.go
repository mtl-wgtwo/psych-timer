package main

import (
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/viper"
)

var c Config

type ClientMessage struct {
	SubjectID string `json:"subjectID,omitempty"`
	Action    string `json:"action,omitempty"`
	Content   string `json:"content,omitempty"`
	KeyCode   byte   `json:"keyCode,omitempty"`
}

type ServerMessage struct {
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message,omitempty"`
}

var client *PsychTimer
var actionChan = make(chan ClientMessage) // broadcast channel
var serverChan = make(chan ServerMessage)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// MapServer does stuff
func MapServer(root map[string]string, prefix string) http.Handler {
	return &mapHandler{root, prefix}
}

type mapHandler struct {
	root   map[string]string
	prefix string
}

func (m *mapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upath := strings.TrimPrefix(r.URL.Path, "/")
	if upath == "" {
		upath = "index.html"
	}
	upath = strings.TrimPrefix(m.prefix, "/") + upath
	ctype := mime.TypeByExtension(filepath.Ext(upath))
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Feature-Policy", "autoplay 'self'")
	fmt.Fprint(w, m.root[upath])
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Upgrade initial GET request to a websocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	// Make sure we close the connection when the function returns
	defer ws.Close()

	log.Debug("Upgraded to ws!")
	// Register our new client
	client = NewPsychTimer(c, ws, serverChan)
	client.ch <- ServerMessage{
		Kind:    "INSTRUCTIONS",
		Message: client.config.Instructions,
	}

	for {
		var msg ClientMessage
		// Read in a new message as JSON and map it to a Message object
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("error: %v", err)
			client = nil
			break
		}
		log.Debugf("Received message %+v\n", msg)
		// Send the newly received message to the broadcast channel
		actionChan <- msg
	}
}

func handleActions() {
	for {
		// Grab the next message from the broadcast channel
		msg := <-actionChan
		log.Debugf("Handling message %+v\n", msg)

		switch msg.Action {
		case "START":
			go client.RunOne(msg.SubjectID)
		case "CANCEL":
			client.Cancel(msg.SubjectID)
		case "KEY":
			log.Debugf("Received key %s (keycode %d) for subject %s\n", msg.Content, msg.KeyCode, msg.SubjectID)
			client.AddKey(msg.Content, msg.KeyCode)
		case "CONTINUE":
			client.Continue()
		default:
			log.Debugln("Unknown message from the client: ", msg.Action)
		}
	}
}

func handleServerMessages() {
	for {
		msg := <-serverChan
		log.Debugf("Server message %+v\n", msg)

		err := client.conn.WriteJSON(msg)
		if err != nil {
			log.Printf("error: %v", err)
			client.conn.Close()
		}
	}
}

func sleepAndOpen() {
	time.Sleep(time.Duration(200) * time.Millisecond)
	open.Start("http://localhost:8080")
}

func main() {
	usage := `Psych Timer
	
Usage:
	psych-timer <config> [--debug]
	psych-timer -h | --help
	psych-timer --version

Options:
	-h --help     Show this screen.
	--version     Show version.
	--debug       Turn on debug messages.
`

	arguments, _ := docopt.ParseDoc(usage)
	log.Debugf("%+v\n", arguments)

	if arguments["--debug"].(bool) {
		log.SetLevel(log.DebugLevel)
	}

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

	log.Debugf("%+v\n", c)

	http.Handle("/", MapServer(Data, "/static/"))

	// Configure websocket route
	http.HandleFunc("/ws", handleConnections)

	go handleActions()
	go handleServerMessages()
	go sleepAndOpen()
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
