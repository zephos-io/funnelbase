package cache

import (
	"context"
	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

func InitialiseClient() *Cache {
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	return &Cache{client}
}
