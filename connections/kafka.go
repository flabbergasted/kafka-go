package connections

import (
	"fmt"
	"os"

	"github.com/confluentinc/confluent-kafka-go/kafka"
)

//KafkaConnection controls the underlying kafka consumer/producer.
type KafkaConnection struct {
	Cleanup  chan int
	consumer *kafka.Consumer
	producer *kafka.Producer
	logger   ILogger
}

var groupid = 1
var topic = "playerPosition"

//NewKafkaConnection creates a new kafka connection
func NewKafkaConnection(logger ILogger, brokerList string) (*KafkaConnection, error) {
	var err error
	kconn := KafkaConnection{}
	kconn.logger = logger
	kconn.Cleanup = make(chan int)
	kconn.consumer, err = kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":               brokerList,
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
		kconn.logger.LogError(err)
		return nil, err
	}

	kconn.producer, err = kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers":      brokerList,
		"linger.ms":              0,
		"socket.nagle.disable":   true,
		"socket.blocking.max.ms": 1,
	})
	if err != nil {
		kconn.logger.LogError(err)
		return nil, err
	}

	go kconn.producerDeliveryReports()
	return &kconn, nil
}

//Listen will invoke 'callback' on every message from the kafka consumer
func (kconn *KafkaConnection) Listen(callback func([]byte)) {
	topics := []string{topic}
	err := kconn.consumer.SubscribeTopics(topics, nil)
	if err != nil {
		kconn.logger.LogError(err)
		return
	}
	kconn.logger.Log("Consumer created and subscribed")

	go func() {
		defer func() {
			kconn.logger.Log("Closing Kafka Producer/Consumer")
			kconn.producer.Close()
			kconn.consumer.Close()
		}()
		for {
			select {
			case ev := <-kconn.consumer.Events():
				switch e := ev.(type) {
				case kafka.AssignedPartitions:
					fmt.Fprintf(os.Stderr, "%% %v\n", e)
					kconn.consumer.Assign(e.Partitions)
				case kafka.RevokedPartitions:
					fmt.Fprintf(os.Stderr, "%% %v\n", e)
					kconn.consumer.Unassign()
				case *kafka.Message:
					//kconn.logger.Log(fmt.Sprintf("%% Message on %s:\n%s\n",
					//	e.TopicPartition, e.Value))
					callback(e.Value) //call passed in callback on regular kafka message
				case kafka.PartitionEOF:
					//fmt.Printf("%% Reached %v\n", e)
				case kafka.Error:
					kconn.logger.LogError(e)
					return
				}
			case <-kconn.Cleanup:
				return
			}
		}
	}()
}

//Send sends the message through the producer to kafka
func (kconn *KafkaConnection) Send(msg []byte) {
	defer func() {
        if r := recover(); r != nil {
            kconn.logger.Log("Recovered in Kafka.Send")
        }
    }()
	//kconn.logger.Log("kafka.go sending message")
	kconn.producer.ProduceChannel() <- &kafka.Message{TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny}, Value: msg}
}

//KClose returns all goroutines and closes connections to kafka
func KClose(kconn *KafkaConnection) {
	close(kconn.Cleanup)
}

//producerDeliveryReports will log delivery reports from the producer
func (kconn *KafkaConnection) producerDeliveryReports() {
	for e := range kconn.producer.Events() {
		switch ev := e.(type) {
		case *kafka.Message:
			if ev.TopicPartition.Error != nil {
				fmt.Printf("Delivery failed: %v\n", ev.TopicPartition)
			} else {
				//fmt.Printf("Delivered message to %v %s\n", ev.TopicPartition, time.Now())
			}
		}
	}
	kconn.logger.Log("Closing delivery report handler")
}
