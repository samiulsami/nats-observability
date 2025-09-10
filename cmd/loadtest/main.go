package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
)

var (
	podName       = os.Getenv("POD_NAME")
	natsURL       string
	natsCredsFile string
	producers     int
	consumers     int
	slowConsumers int

	producerRate        int
	consumerRate        int
	slowConsumerRate    int
	msgSize             int
	subjectCommonPrefix string
	streamMaxMsgs       int64
	streamMaxBytes      int64
)

type Stats struct {
	received int64
	errors   int64
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "loadtest",
		Short: "NATS storage cleanup tool - drains all messages from all streams",
		Run:   runLoadTest,
	}

	rootCmd.Flags().StringVar(&natsURL, "nats-url", "nats://ace-nats.ace.svc.cluster.local:4222", "NATS server URL")
	rootCmd.Flags().StringVar(&natsCredsFile, "creds", "", "NATS credentials file path")
	rootCmd.Flags().IntVar(&producers, "producers", 50, "Number of producers")
	rootCmd.Flags().IntVar(&consumers, "consumers", 20, "Number of cleanup consumers")
	rootCmd.Flags().IntVar(&slowConsumers, "slow-consumers", 20, "Number of slow consumers")

	rootCmd.Flags().IntVar(&producerRate, "producer-rate", 100, "Messages per second per producer")
	rootCmd.Flags().IntVar(&consumerRate, "consumer-rate", 50, "Messages per second per fast consumer")
	rootCmd.Flags().IntVar(&slowConsumerRate, "slow-consumer-rate", 10, "Messages per second per slow consumer")
	rootCmd.Flags().IntVar(&msgSize, "size", 1024, "Message size in bytes")
	rootCmd.Flags().StringVar(&subjectCommonPrefix, "subject", "loadtest", "Subject name")
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
	subjectCommonPrefix = fmt.Sprintf("%s_%s_%s", subjectCommonPrefix, podName, newuuid.String())

	log.Printf("Starting NATS storage cleanup...")
	log.Printf("NATS URL: %s", natsURL)
	log.Printf("Cleanup consumers per stream: %d", consumers)

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

	// streamName := subjectCommonPrefix + "_stream"
	// if waitErr := wait.ExponentialBackoff(wait.Backoff{
	// 	Duration: 500 * time.Millisecond,
	// 	Factor:   1.5,
	// 	Jitter:   0.5,
	// 	Cap:      10 * time.Second,
	// 	Steps:    5,
	// }, func() (bool, error) {
	// 	_, err = js.AddStream(&nats.StreamConfig{
	// 		Name:      streamName,
	// 		Subjects:  []string{subjectCommonPrefix + ".>"},
	// 		Retention: nats.LimitsPolicy,
	// 		MaxMsgs:   streamMaxMsgs,
	// 		MaxBytes:  streamMaxBytes,
	// 		MaxAge:    24 * time.Hour,
	// 	})
	// 	return err == nil || errors.Is(err, nats.ErrStreamNameAlreadyInUse), nil
	// }); waitErr != nil && !wait.Interrupted(waitErr) {
	// 	log.Fatalf("Failed to create or verify stream: %v", err)
	// }
	//
	// if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
	// 	log.Fatalf("Failed to create stream: %v", err)
	// }

	stats := &Stats{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup

	// Get all existing streams
	streamNames, err := getAllStreams(js)
	if err != nil {
		log.Fatalf("Failed to get streams: %v", err)
	}

	if len(streamNames) == 0 {
		log.Printf("No streams found - nothing to clean up")
		return
	}

	log.Printf("Found %d streams to clean up: %v", len(streamNames), streamNames)

	// Start statistics reporter
	wg.Go(func() {
		reportStats(ctx, stats)
	})

	// Create consumers for each stream
	for i, streamName := range streamNames {
		for j := 0; j < consumers; j++ {
			streamIdx := i
			consumerIdx := j
			wg.Go(func() {
				runStreamConsumer(ctx, nc, js, streamIdx, consumerIdx, streamName, stats)
			})
		}
	}

	// for i := 0; i < slowConsumers; i++ {
	// 	id := i
	// 	wg.Go(func() {
	// 		runConsumer(ctx, nc, js, id+consumers, true, stats)
	// 	})
	// }

	// for i := 0; i < producers; i++ {
	// 	id := i
	// 	wg.Go(func() {
	// 		runProducer(ctx, nc, js, id, stats)
	// 	})
	// }

	log.Println("Storage cleanup started. Press Ctrl+C to stop.")

	select {
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down gracefully...", sig)
		cancel()
	case <-ctx.Done():
		log.Println("Context cancelled, shutting down...")
	}

	log.Println("Waiting for goroutines to finish...")
	wg.Wait()
	log.Println("Storage cleanup stopped.")
}

// func runProducer(ctx context.Context, nc *nats.Conn, js nats.JetStreamContext, id int, stats *Stats) {
// 	ticker := time.NewTicker(time.Second / time.Duration(producerRate))
// 	defer ticker.Stop()
//
// 	subject := fmt.Sprintf("%s.producer.%d", subjectCommonPrefix, id)
//
// 	log.Printf("Producer %d: Started", id)
// 	defer log.Printf("Producer %d: Stopped", id)
//
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			return
// 		case <-ticker.C:
// 			message := generateMessage(msgSize)
// 			_, err := js.Publish(subject, message)
// 			if err != nil {
// 				atomic.AddInt64(&stats.errors, 1)
// 				log.Printf("Producer %d: Publish error: %v", id, err)
// 			} else {
// 				atomic.AddInt64(&stats.sent, 1)
// 			}
// 		}
// 	}
// }

func reportStats(ctx context.Context, stats *Stats) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	lastReceived := int64(0)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			received := atomic.LoadInt64(&stats.received)
			errors := atomic.LoadInt64(&stats.errors)
			rate := (received - lastReceived) / 10
			lastReceived = received

			log.Printf("STATS: Total processed: %d, Errors: %d, Rate: %d msg/s", received, errors, rate)
		}
	}
}

func getAllStreams(js nats.JetStreamContext) ([]string, error) {
	var streamNames []string

	for info := range js.StreamsInfo() {
		if info == nil {
			break
		}
		streamNames = append(streamNames, info.Config.Name)
		log.Printf("Found stream: %s with %d messages (%d bytes)",
			info.Config.Name, info.State.Msgs, info.State.Bytes)
	}

	return streamNames, nil
}

func runStreamConsumer(ctx context.Context, nc *nats.Conn, js nats.JetStreamContext, streamIdx, consumerIdx int, streamName string, stats *Stats) {
	newuuid, err := uuid.NewRandom()
	if err != nil {
		log.Printf("StreamConsumer %d-%d: Failed to generate UUID: %v", streamIdx, consumerIdx, err)
		return
	}

	consumerName := fmt.Sprintf("cleanup_%s_s%d_c%d_%d_%s", podName, streamIdx, consumerIdx, time.Now().UnixNano(), newuuid.String())

	log.Printf("StreamConsumer %d-%d: Creating consumer %s for stream %s", streamIdx, consumerIdx, consumerName, streamName)

	// Create consumer directly on the stream with DeliverAll policy
	consumerConfig := &nats.ConsumerConfig{
		Durable:       consumerName,
		DeliverPolicy: nats.DeliverAllPolicy,
		AckPolicy:     nats.AckExplicitPolicy,
		MaxWaiting:    1000,
		MaxAckPending: 1000,
	}

	consumerInfo, err := js.AddConsumer(streamName, consumerConfig)
	if err != nil {
		log.Printf("StreamConsumer %d-%d: Failed to create consumer for stream %s: %v", streamIdx, consumerIdx, streamName, err)
		return
	}

	sub, err := js.PullSubscribe("", consumerName, nats.Bind(streamName, consumerName))
	if err != nil {
		log.Printf("StreamConsumer %d-%d: Failed to create pull subscription for stream %s: %v", streamIdx, consumerIdx, streamName, err)
		return
	}

	defer func() {
		if err := sub.Unsubscribe(); err != nil {
			log.Printf("StreamConsumer %d-%d: Unsubscribe error for stream %s: %v", streamIdx, consumerIdx, streamName, err)
		}
		// Clean up the consumer
		if err := js.DeleteConsumer(streamName, consumerName); err != nil {
			log.Printf("StreamConsumer %d-%d: Failed to delete consumer %s from stream %s: %v", streamIdx, consumerIdx, consumerName, streamName, err)
		}
	}()

	log.Printf("StreamConsumer %d-%d: Started cleanup for stream %s (consumer: %s)", streamIdx, consumerIdx, streamName, consumerInfo.Name)
	defer log.Printf("StreamConsumer %d-%d: Stopped cleanup for stream %s", streamIdx, consumerIdx, streamName)

	batchSize := 500
	totalProcessed := int64(0)

	for {
		select {
		case <-ctx.Done():
			log.Printf("StreamConsumer %d-%d: Processed %d total messages from stream %s", streamIdx, consumerIdx, totalProcessed, streamName)
			return
		default:
			msgs, err := sub.Fetch(batchSize, nats.MaxWait(2*time.Second))
			if err != nil {
				if err == nats.ErrTimeout {
					// Check if stream is empty
					info, infoErr := js.StreamInfo(streamName)
					if infoErr == nil && info.State.Msgs == 0 {
						log.Printf("StreamConsumer %d-%d: Stream %s is now empty, processed %d total messages", streamIdx, consumerIdx, streamName, totalProcessed)
						return
					}
					continue
				}
				atomic.AddInt64(&stats.errors, 1)
				log.Printf("StreamConsumer %d-%d: Fetch error from stream %s: %v", streamIdx, consumerIdx, streamName, err)
				time.Sleep(500 * time.Millisecond)
				continue
			}

			if len(msgs) == 0 {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			batchProcessed := 0
			for _, msg := range msgs {
				select {
				case <-ctx.Done():
					log.Printf("StreamConsumer %d-%d: Processed %d total messages from stream %s", streamIdx, consumerIdx, totalProcessed, streamName)
					return
				default:
					if err := msg.Ack(); err != nil {
						atomic.AddInt64(&stats.errors, 1)
						log.Printf("StreamConsumer %d-%d: Ack error for stream %s: %v", streamIdx, consumerIdx, streamName, err)
					} else {
						atomic.AddInt64(&stats.received, 1)
						batchProcessed++
						totalProcessed++
					}
				}
			}

			if batchProcessed > 0 {
				log.Printf("StreamConsumer %d-%d: Processed batch of %d messages from stream %s (total: %d)", streamIdx, consumerIdx, batchProcessed, streamName, totalProcessed)
			}
		}
	}
}

// func runConsumer(ctx context.Context, nc *nats.Conn, js nats.JetStreamContext, id int, isSlow bool, stats *Stats) {
// 	subject := ">"
// 	consumerType := "cleanup"
//
// 	newuuid, err := uuid.NewRandom()
// 	if err != nil {
// 		log.Printf("Consumer %d: Failed to generate UUID: %v", id, err)
// 		return
// 	}
//
// 	consumerName := fmt.Sprintf("%s_%s_c%d_%d_%s", consumerType, podName, id, time.Now().UnixNano(), newuuid.String())
//
// 	log.Printf("Consumer %d: Creating promiscuous consumer %s for subject %s", id, consumerName, subject)
//
// 	sub, err := js.PullSubscribe(subject, consumerName,
// 		nats.PullMaxWaiting(1000),
// 		nats.DeliverAll(),
// 		nats.AckExplicit(),
// 	)
// 	if err != nil {
// 		log.Printf("Consumer %d: Failed to create pull subscription: %v", id, err)
// 		return
// 	}
// 	defer func() {
// 		if err := sub.Unsubscribe(); err != nil {
// 			log.Printf("Consumer %d: Unsubscribe error: %v", id, err)
// 		}
// 	}()
//
// 	log.Printf("Consumer %d: Started aggressive cleanup consumer", id)
// 	defer log.Printf("Consumer %d: Stopped cleanup consumer", id)
//
// 	batchSize := 100
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			return
// 		default:
// 			msgs, err := sub.Fetch(batchSize, nats.MaxWait(1*time.Second))
// 			if err != nil {
// 				if err == nats.ErrTimeout {
// 					continue
// 				}
// 				atomic.AddInt64(&stats.errors, 1)
// 				log.Printf("Consumer %d: Fetch error: %v", id, err)
// 				time.Sleep(100 * time.Millisecond)
// 				continue
// 			}
//
// 			if len(msgs) == 0 {
// 				time.Sleep(100 * time.Millisecond)
// 				continue
// 			}
//
// 			for _, msg := range msgs {
// 				select {
// 				case <-ctx.Done():
// 					return
// 				default:
// 					atomic.AddInt64(&stats.received, 1)
// 					if err := msg.Ack(); err != nil {
// 						atomic.AddInt64(&stats.errors, 1)
// 						log.Printf("Consumer %d: Ack error: %v", id, err)
// 					}
// 				}
// 			}
//
// 			if len(msgs) > 0 {
// 				log.Printf("Consumer %d: Processed batch of %d messages", id, len(msgs))
// 			}
// 		}
// 	}
// }

// func generateMessage(size int) []byte {
// 	data := map[string]any{
// 		"timestamp": time.Now().UnixNano(),
// 		"id":        rand.Int63(),
// 		"payload":   randomString(size - 100),
// 	}
//
// 	msg, _ := json.Marshal(data)
// 	return msg
// }
//
// func randomString(length int) string {
// 	if length <= 0 {
// 		return ""
// 	}
// 	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
// 	result := make([]byte, length)
// 	for i := range result {
// 		result[i] = chars[rand.Intn(len(chars))]
// 	}
// 	return string(result)
// }
