package main

import _ "net/http/pprof"
import _ "net/http"
import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/gorilla/websocket"
)

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

var groupid = 1
var upgrader = websocket.Upgrader{} // use default options

var topic = "playerPosition"

func main() {
	http.HandleFunc("/ws", webSocket)
	http.HandleFunc("/index", index)
	err := http.ListenAndServe(":1588", nil)
	if err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
func webSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	cancelChan := make(chan int)
	if err != nil {
		log.Println(err)
		return
	}
	go clientToKafka(conn, cancelChan)
	go kafkaToClient(conn, cancelChan)
	return
}
func index(w http.ResponseWriter, r *http.Request) {
	pageBytes, err := ioutil.ReadFile("html/index.html")

	if err != nil {
		fmt.Fprintf(w, err.Error())
	}
	w.Write(pageBytes)
	fmt.Fprintf(w, "Hi there, I love %s!", r.URL.Path[1:])
	return
}
func readFromKafkaIntoChannel(c *kafka.Consumer, kafkaMessage chan []byte, cancelChan <-chan int) {
	defer func() {
		close(kafkaMessage)
		fmt.Println("Closing read from kafka into channel")
	}()
	topics := []string{topic}
	err := c.SubscribeTopics(topics, nil)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	fmt.Println("Consumer created and subscribed")

	for {
		select {
		case ev := <-c.Events():
			switch e := ev.(type) {
			case kafka.AssignedPartitions:
				fmt.Fprintf(os.Stderr, "%% %v\n", e)
				c.Assign(e.Partitions)
			case kafka.RevokedPartitions:
				fmt.Fprintf(os.Stderr, "%% %v\n", e)
				c.Unassign()
			case *kafka.Message:
				fmt.Printf("%% Message on %s:\n%s\n",
					e.TopicPartition, e.Value)
				kafkaMessage <- e.Value
			case kafka.PartitionEOF:
				fmt.Printf("%% Reached %v\n", e)
			case kafka.Error:
				fmt.Fprintf(os.Stderr, "%% Error: %v\n", e)
				close(kafkaMessage)
				return
			}
		case <-cancelChan:
			return
		}
	}
}
func sendKafkaMessage(p *kafka.Producer, msg []byte) {
	//write to kafka
	fmt.Printf("%s!", msg)

	// Produce messages to topic (asynchronously)
	//Channel based producer
	p.ProduceChannel() <- &kafka.Message{TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny}, Value: msg}

	// Function Based Producer
	// p.Produce(&kafka.Message{
	// 	TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
	// 	Value:          msg,
	// }, nil)

	// // Wait for message deliveries
	// p.Flush(30)
}

func clientToKafka(conn *websocket.Conn, cancelChan chan int) {
	ticker := time.NewTicker(pingPeriod)
	p, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers":      "localhost:9092",
		"linger.ms":              0,
		"socket.nagle.disable":   true,
		"socket.blocking.max.ms": 1,
	})

	defer func() {
		fmt.Println("Pong Timeout, closing web socket and kafka producer")
		p.Close()
		ticker.Stop()
		conn.Close()
		close(cancelChan)
	}()

	// Delivery report handler for produced messages
	go func() {
		for e := range p.Events() {
			switch ev := e.(type) {
			case *kafka.Message:
				if ev.TopicPartition.Error != nil {
					fmt.Printf("Delivery failed: %v\n", ev.TopicPartition)
				} else {
					fmt.Printf("Delivered message to %v %s\n", ev.TopicPartition, time.Now())
				}
			}
		}
		fmt.Println("Closing delivery report handler")
	}()

	if err != nil {
		panic(err)
	}
	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		fmt.Println("Pong Received")
		return nil
	})
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		fmt.Printf("Received message from WS %s @%s\n", message, time.Now())
		sendKafkaMessage(p, message)
	}
}
func kafkaToClient(conn *websocket.Conn, cancelChan <-chan int) {
	kafkaMessage := make(chan []byte)
	ticker := time.NewTicker(pingPeriod)
	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":               "localhost:9092",
		"group.id":                        groupid,
		"enable.auto.commit":              false,
		"session.timeout.ms":              6000,
		"go.events.channel.enable":        true,
		"go.application.rebalance.enable": true,
		"socket.nagle.disable":            true,
		"fetch.wait.max.ms":               1,
		"fetch.error.backoff.ms":          0,
		"socket.blocking.max.ms":          1,
		"default.topic.config":            kafka.ConfigMap{"auto.offset.reset": "latest"}})
	groupid++
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create consumer: %s\n", err)
		os.Exit(1)
	}
	defer func() {
		fmt.Println("Exiting Consumer")
		c.Close()
		ticker.Stop()
		conn.Close()
	}()
	go readFromKafkaIntoChannel(c, kafkaMessage, cancelChan)
	for {
		select {
		case message, ok := <-kafkaMessage: //on kafka consumer channel receive
			fmt.Println("Consumer received message")
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Error
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				fmt.Println(err.Error())
				return
			}
		case <-ticker.C:
			fmt.Println("Send Ping")
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-cancelChan:
			return
		}
	}
}
