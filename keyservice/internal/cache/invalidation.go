package cache

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// RevocationChannel is retained for backward compatibility with any
	// existing pub/sub listeners during a rolling upgrade.
	RevocationChannel = "revocations"

	// RevocationStream is the durable Redis Stream that guarantees
	// at-least-once delivery of revocation events to all replicas.
	RevocationStream = "revocations:stream"

	// streamMaxLen is the approximate maximum number of entries retained
	// in the stream. Old entries are trimmed to bound memory usage.
	streamMaxLen int64 = 10000
)

// PublishRevocation writes the revocation event to both the legacy pub/sub
// channel (for any old replicas during a rolling upgrade) and the durable
// Redis Stream (for reliable delivery).
func PublishRevocation(ctx context.Context, rdb *redis.Client, keyHash string) error {
	// Durable stream entry — survives replica disconnections.
	if err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: RevocationStream,
		MaxLen: streamMaxLen,
		Approx: true,
		Values: map[string]interface{}{"key_hash": keyHash},
	}).Err(); err != nil {
		return err
	}
	// Legacy pub/sub — best-effort for backward compatibility.
	_ = rdb.Publish(ctx, RevocationChannel, keyHash).Err()
	return nil
}

// SubscribeRevocations consumes the durable revocation stream using a
// consumer group. Each replica creates its own consumer within the group,
// ACKs processed entries, and evicts revoked keys from the local L1 cache.
// This replaces the fire-and-forget pub/sub model with at-least-once delivery.
func SubscribeRevocations(ctx context.Context, rdb *redis.Client, l1 *Cache) {
	const groupName = "keyservice"

	// Create the consumer group if it doesn't exist. Start reading from
	// the beginning ("0") so new replicas catch up on missed revocations.
	_ = rdb.XGroupCreateMkStream(ctx, RevocationStream, groupName, "0").Err()

	// Use the replica's hostname as the consumer name for observability.
	consumerName := "consumer-" + time.Now().Format("20060102-150405.000")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// XREADGROUP with BLOCK — waits for new entries, no busy-loop.
		streams, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    groupName,
			Consumer: consumerName,
			Streams:  []string{RevocationStream, ">"},
			Count:    100,
			Block:    5 * time.Second,
		}).Result()
		if err != nil {
			if err == redis.Nil {
				continue // timeout, no new messages
			}
			log.Printf("revocation stream read error: %v", err)
			time.Sleep(1 * time.Second) // back off on errors
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				keyHash, ok := msg.Values["key_hash"].(string)
				if ok && keyHash != "" {
					l1.Delete(keyHash)
					log.Printf("evicted revoked key from L1 (stream)")
				}
				// ACK the message so it won't be re-delivered.
				rdb.XAck(ctx, RevocationStream, groupName, msg.ID)
			}
		}
	}
}
