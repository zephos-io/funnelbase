package redis

import (
	"github.com/redis/go-redis/v9"
)

type Redis struct {
	client *redis.Client
}

func InitialiseClient() *Redis {
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	return &Redis{client}
}
