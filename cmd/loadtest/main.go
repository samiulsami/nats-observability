package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	wait "k8s.io/apimachinery/pkg/util/wait"
)

var (
	podName       = os.Getenv("POD_NAME")
	natsURL       string
	natsCredsFile string
	producers     int
	consumers     int
	slowConsumers int

	producerRate     int
	consumerRate     int
	slowConsumerRate int
	msgSize          int
	subjectName      string
	streamMaxMsgs    int64
	streamMaxBytes   int64
)

type Stats struct {
	sent     int64
	received int64
	errors   int64
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "loadtest",
		Short: "NATS load testing tool",
		Run:   runLoadTest,
	}

	rootCmd.Flags().StringVar(&natsURL, "nats-url", "nats://ace-nats.ace.svc.cluster.local:4222", "NATS server URL")
	rootCmd.Flags().StringVar(&natsCredsFile, "creds", "", "NATS credentials file path")
	rootCmd.Flags().IntVar(&producers, "producers", 50, "Number of producers")
	rootCmd.Flags().IntVar(&consumers, "consumers", 50, "Number of fast consumers")
	rootCmd.Flags().IntVar(&slowConsumers, "slow-consumers", 20, "Number of slow consumers")

	rootCmd.Flags().IntVar(&producerRate, "producer-rate", 100, "Messages per second per producer")
	rootCmd.Flags().IntVar(&consumerRate, "consumer-rate", 50, "Messages per second per fast consumer")
	rootCmd.Flags().IntVar(&slowConsumerRate, "slow-consumer-rate", 10, "Messages per second per slow consumer")
	rootCmd.Flags().IntVar(&msgSize, "size", 1024, "Message size in bytes")
	rootCmd.Flags().StringVar(&subjectName, "subject", "loadtest", "Subject name")
	rootCmd.Flags().Int64Var(&streamMaxMsgs, "stream-max-msgs", 1000000, "Maximum messages in stream")
	rootCmd.Flags().Int64Var(&streamMaxBytes, "stream-max-bytes", 1024*1024*1024, "Maximum bytes in stream (1GB default)")

	if podName == "" {
		podName = "local"
	}

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func runLoadTest(cmd *cobra.Command, args []string) {
	newuuid, err := uuid.NewRandom()
	if err != nil {
		log.Fatalf("Failed to generate UUID: %v", err)
	}
	subjectName = fmt.Sprintf("%s_%s_%s", subjectName, podName, newuuid.String())

	log.Printf("Starting NATS load test...")
	log.Printf("NATS URL: %s", natsURL)
	log.Printf("Producers: %d, Fast Consumers: %d, Slow Consumers: %d", producers, consumers, slowConsumers)
	log.Printf("Producer Rate: %d msg/s, Consumer Rate: %d/%d msg/s (fast/slow), Size: %d bytes", producerRate, consumerRate, slowConsumerRate, msgSize)
	log.Printf("Subject: %s", subjectName)

	var nc *nats.Conn

	if natsCredsFile != "" {
		nc, err = nats.Connect(natsURL,
			nats.UserCredentials(natsCredsFile),
			nats.Name("LoadTester"),
			nats.ReconnectWait(time.Second),
			nats.MaxReconnects(-1),
		)
	} else {
		nc, err = nats.Connect(natsURL,
			nats.Name("LoadTester"),
			nats.ReconnectWait(time.Second),
			nats.MaxReconnects(-1),
		)
	}
	if err != nil {
		log.Fatal("Failed to connect to NATS:", err)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		log.Fatal("Failed to get JetStream context:", err)
	}

	streamName := subjectName + "_stream"
	if waitErr := wait.ExponentialBackoff(wait.Backoff{
		Duration: 500 * time.Millisecond,
		Factor:   1.5,
		Jitter:   0.5,
		Cap:      10 * time.Second,
		Steps:    5,
	}, func() (bool, error) {
		_, err = js.AddStream(&nats.StreamConfig{
			Name:      streamName,
			Subjects:  []string{subjectName + ".>"},
			Retention: nats.LimitsPolicy,
			MaxMsgs:   streamMaxMsgs,
			MaxBytes:  streamMaxBytes,
		})
		return err == nil || errors.Is(err, nats.ErrStreamNameAlreadyInUse), nil
	}); waitErr != nil && !wait.Interrupted(waitErr) {
		log.Fatalf("Failed to create or verify stream: %v", err)
	}

	if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		log.Fatalf("Failed to create stream: %v", err)
	}

	stats := &Stats{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	for i := 0; i < consumers; i++ {
		id := i
		wg.Go(func() {
			runConsumer(ctx, nc, js, id, false, stats)
		})
	}

	for i := 0; i < slowConsumers; i++ {
		id := i
		wg.Go(func() {
			runConsumer(ctx, nc, js, id+consumers, true, stats)
		})
	}

	for i := 0; i < producers; i++ {
		id := i
		wg.Go(func() {
			runProducer(ctx, nc, js, id, stats)
		})
	}

	log.Println("Load test started. Press Ctrl+C to stop.")

	select {
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down gracefully...", sig)
		cancel()
	case <-ctx.Done():
		log.Println("Context cancelled, shutting down...")
	}

	log.Println("Waiting for goroutines to finish...")
	wg.Wait()
	log.Println("Load test stopped.")
}

func runProducer(ctx context.Context, nc *nats.Conn, js nats.JetStreamContext, id int, stats *Stats) {
	ticker := time.NewTicker(time.Second / time.Duration(producerRate))
	defer ticker.Stop()

	subject := fmt.Sprintf("%s.producer.%d", subjectName, id)

	log.Printf("Producer %d: Started", id)
	defer log.Printf("Producer %d: Stopped", id)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			message := generateMessage(msgSize)
			_, err := js.Publish(subject, message)
			if err != nil {
				atomic.AddInt64(&stats.errors, 1)
				log.Printf("Producer %d: Publish error: %v", id, err)
			} else {
				atomic.AddInt64(&stats.sent, 1)
			}
		}
	}
}

func runConsumer(ctx context.Context, nc *nats.Conn, js nats.JetStreamContext, id int, isSlow bool, stats *Stats) {
	subject := fmt.Sprintf("%s.messages", subjectName)
	consumerType := "fast"
	rate := consumerRate
	if isSlow {
		consumerType = "slow"
		rate = slowConsumerRate
	}

	newuuid, err := uuid.NewRandom()
	if err != nil {
		log.Printf("Consumer %d: Failed to generate UUID: %v", id, err)
		return
	}

	consumerName := fmt.Sprintf("%s_%s_c%d_%d_%s", consumerType, podName, id, time.Now().UnixNano(), newuuid.String())

	log.Printf("subject name: %s, consumer name: %s\n", subject, consumerName)
	sub, err := js.PullSubscribe(subject, consumerName, nats.PullMaxWaiting(1000))
	if err != nil {
		log.Printf("Consumer %d (%s): Failed to create pull subscription: %v", id, consumerName, err)
		return
	}
	defer func() {
		if err := sub.Unsubscribe(); err != nil {
			log.Printf("Consumer %d (%s): Unsubscribe error: %v", id, consumerType, err)
		}
	}()

	ticker := time.NewTicker(time.Second / time.Duration(rate))
	defer ticker.Stop()

	log.Printf("Consumer %d (%s): Started at %d msg/sec", id, consumerType, rate)
	defer log.Printf("Consumer %d (%s): Stopped", id, consumerType)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msgs, err := sub.Fetch(1, nats.MaxWait(500*time.Millisecond))
			if err != nil && err != nats.ErrTimeout {
				atomic.AddInt64(&stats.errors, 1)
				continue
			}

			for _, msg := range msgs {
				select {
				case <-ctx.Done():
					return
				default:
					atomic.AddInt64(&stats.received, 1)

					if err := msg.Ack(); err != nil {
						atomic.AddInt64(&stats.errors, 1)
					}
				}
			}
		}
	}
}

func generateMessage(size int) []byte {
	data := map[string]any{
		"timestamp": time.Now().UnixNano(),
		"id":        rand.Int63(),
		"payload":   randomString(size - 100),
	}

	msg, _ := json.Marshal(data)
	return msg
}

func randomString(length int) string {
	if length <= 0 {
		return ""
	}
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}
