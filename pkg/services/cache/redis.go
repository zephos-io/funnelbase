package cache

import (
	"github.com/redis/go-redis/v9"
)

// TODO: accept flags/env vars for this connection
func InitialiseRedis() (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	res := client.Ping(ctx)

	if res.Err() == nil {
		logger.Info().Msg("successfully connected to redis")
	}

	return client, res.Err()
}
