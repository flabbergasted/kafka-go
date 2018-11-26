package connections

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{} // use default options

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

//WebsocketConnection controls the underlying websocket and calls a provided callback function on message receive.
type WebsocketConnection struct {
	connection  *websocket.Conn
	Cleanup     chan int
	Closing  chan int
	sendMessage chan []byte
	logger      ILogger
}

//NewWebsocketConnection creates and initializes a WebsocketConnection struct, kicking off the ping/pong heartbeat
func NewWebsocketConnection(w http.ResponseWriter, r *http.Request, newLogger ILogger) (*WebsocketConnection, error) {
	var wconn = WebsocketConnection{}
	var err error
	wconn.connection, err = upgrader.Upgrade(w, r, nil)
	wconn.sendMessage = make(chan []byte)
	wconn.Cleanup = make(chan int)
	wconn.Closing = make(chan int)
	wconn.logger = newLogger

	if err != nil {
		wconn.logger.LogError(err)
		return nil, err
	}

	go wconn.writeLoop()

	return &wconn, nil
}

//Listen creates a listener that invokes the provided callback when a message is received on the websocket
func (wconn *WebsocketConnection) Listen(callback func([]byte)) {
	go func() {
		for {
			_, message, err := wconn.connection.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					wconn.logger.LogError(err)
				}
				return
			}
			wconn.logger.Log(fmt.Sprintf("Received message from WS %s @%s\n", message, time.Now()))
			callback(message)
		}
	}()
}

//Send sends the message through the websocket connection
func (wconn *WebsocketConnection) Send(msg []byte) {
	wconn.sendMessage <- msg
}

//WSClose Closes the websocket and cleans up all running go routines
func WSClose(w *WebsocketConnection) {
	close(w.Cleanup)
}

//takes care of writing messages and periodic pings to the underlying websocket
func (wconn *WebsocketConnection) writeLoop() {
	ticker := time.NewTicker(pingPeriod)

	defer func() {
		ticker.Stop()
		wconn.connection.SetWriteDeadline(time.Now().Add(writeWait))
		wconn.connection.WriteMessage(websocket.CloseMessage, []byte{})
		wconn.connection.Close()
		wconn.logger.Log("writeLoop closed")
	}()

	wconn.connection.SetReadLimit(maxMessageSize)
	wconn.connection.SetReadDeadline(time.Now().Add(pongWait))
	wconn.connection.SetPongHandler(func(string) error {
		wconn.connection.SetReadDeadline(time.Now().Add(pongWait))
		wconn.logger.Log("Pong Received")
		return nil
	})

	for {
		select {
		case <-ticker.C:
			wconn.logger.Log("Send Ping")
			wconn.connection.SetWriteDeadline(time.Now().Add(writeWait))
			if err := wconn.connection.WriteMessage(websocket.PingMessage, nil); err != nil {
				wconn.logger.LogError(err)
				close(wconn.Closing)
				return
			}
		case <-wconn.Cleanup:
			close(wconn.sendMessage)
			return
		case newMsg := <-wconn.sendMessage:
			wconn.logger.Log("websocket.go sending message")
			wconn.connection.SetWriteDeadline(time.Now().Add(writeWait))

			writer, err := wconn.connection.NextWriter(websocket.TextMessage)
			if err != nil {
				wconn.logger.Log(err.Error())
				close(wconn.Closing)
				return
			}
			writer.Write(newMsg)

			if err := writer.Close(); err != nil {
				wconn.logger.Log(err.Error())
				close(wconn.Closing)
				return
			}
		}
	}
}
