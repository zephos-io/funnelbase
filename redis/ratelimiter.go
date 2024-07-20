package redis

import (
	"fmt"
	"time"
)

func getKey(window time.Duration, key string) string {
	now := time.Now()
	timestamp := fmt.Sprint(now.Truncate(window).Unix())
	return fmt.Sprintf("cache_%s_%s", key, timestamp)
}

func (r *Redis) expireKey(rKey string, expireIn time.Duration) {
	r.client.Expire(ctx, rKey, expireIn)
}

func (r *Redis) Increment(window time.Duration, key string, field string, increment int) (int32, error) {
	now := time.Now()
	timeUntilNextWindow := now.Truncate(window).Add(window).Unix() - now.Unix()

	rKey := getKey(window, key)

	val, err := r.client.HIncrBy(ctx, rKey, field, int64(increment)).Result()
	if err != nil {
		return 0, err
	}

	r.expireKey(rKey, time.Duration(timeUntilNextWindow)*time.Second)

	return int32(val), nil
}

func (r *Redis) Decrement(window time.Duration, key string, field string, decrement int) (int32, error) {
	now := time.Now()
	timeUntilNextWindow := now.Truncate(window).Add(window).Unix() - now.Unix()

	rKey := getKey(window, key)

	val, err := r.client.HIncrBy(ctx, rKey, field, -int64(decrement)).Result()
	if err != nil {
		return 0, err
	}

	r.expireKey(rKey, time.Duration(timeUntilNextWindow)*time.Second)

	return int32(val), nil
}

func (r *Redis) SetLastRequestTime(window time.Duration, key string) (int32, error) {
	now := time.Now()
	timeUntilNextWindow := now.Truncate(window).Add(window).Unix() - now.Unix()

	rKey := getKey(window, key)

	val, err := r.client.HSet(ctx, rKey, "last_request_time", time.Now().Format(time.RFC3339)).Result()
	if err != nil {
		return 0, err
	}

	r.expireKey(rKey, time.Duration(timeUntilNextWindow)*time.Second)

	return int32(val), nil
}

func (r *Redis) Get(window time.Duration, key string, field string) (string, error) {
	rKey := getKey(window, key)

	val, err := r.client.HGet(ctx, rKey, field).Result()
	if err != nil {
		return "", err
	}

	return val, err
}
