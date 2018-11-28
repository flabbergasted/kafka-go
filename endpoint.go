package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http"
	_ "net/http/pprof"
	"os"

	"github.com/flabbergasted/kafka/connections"
)

func main() {
	http.HandleFunc("/ws", webSocket)
	http.HandleFunc("/index", index)
	err := http.ListenAndServe(":1588", nil)
	if err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
func webSocket(w http.ResponseWriter, r *http.Request) {
	logger := connections.NoOpLogger{}
	brokerList := os.Getenv("BROKER_LIST")
	if brokerList == "" {
		brokerList = "localhost:9092"
	}

	fmt.Println(brokerList)
	conn, err := connections.NewWebsocketConnection(w, r, logger)
	if err != nil {
		fmt.Printf("error %v:", err)
	}
	kafkaConn, kerr := connections.NewKafkaConnection(logger, brokerList)
	if kerr != nil {
		fmt.Printf("error %v:", err)
	}
	kafkaConn.Listen(conn.Send) //send kafka messages to websocket
	conn.Listen(kafkaConn.Send) //send websocket messages to kafka

	go func() { //When one connection closes, make sure to close the other
		select {
		case <-kafkaConn.Cleanup:
			connections.WSClose(conn)
			return
		case <-conn.Cleanup:
			connections.KClose(kafkaConn)
			return
		}
	}()
	return
}
func index(w http.ResponseWriter, r *http.Request) {
	htmlLocation := os.Getenv("HTML_LOCATION")
	if htmlLocation == "" {
		htmlLocation = "html"
	}
	fmt.Println(htmlLocation)

	fullFileName := htmlLocation + "/index.html"
	pageBytes, err := ioutil.ReadFile(fullFileName)
	if err != nil {
		fmt.Fprintf(w, err.Error())
	}
	w.Write(pageBytes)
	fmt.Fprintf(w, "Hi there, I love %s!\n", r.URL.Path[1:])
	return
}
