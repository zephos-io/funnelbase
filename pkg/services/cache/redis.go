package cache

import (
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"strconv"
)

// TODO: accept flags/env vars for this connection
func InitialiseRedis() (*redis.Client, error) {
	dbNum, err := strconv.Atoi(viper.GetString("redis_db"))
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(&redis.Options{
		Addr:     viper.GetString("redis_addr"),
		Password: viper.GetString("redis_password"),
		DB:       dbNum,
	})

	res := client.Ping(ctx)

	if res.Err() == nil {
		logger.Info().Msg("successfully connected to redis")
	}

	return client, res.Err()
}
