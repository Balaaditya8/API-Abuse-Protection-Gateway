package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func APIKeyAuthMiddleware(dbpool *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {

		apiKey := c.GetHeader("x-api-key")
		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "missing api key",
			})
			c.Abort()
			return
		}

		query := `
		SELECT tenant_id, active
		FROM api_keys
		WHERE key = $1
		`

		var tenantID int64
		var active bool

		err := dbpool.QueryRow(
			c.Request.Context(),
			query,
			apiKey,
		).Scan(&tenantID, &active)

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid api key",
			})
			c.Abort()
			return
		}

		if !active {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "api key inactive",
			})
			c.Abort()
			return
		}

		c.Set("api_key", apiKey)
		c.Set("tenant_id", tenantID)

		c.Next()
	}
}

func RateLimitMiddleware(rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKeyValue, exists := c.Get("api_key")
		if !exists {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "api key missing from context",
			})
			c.Abort()
			return
		}

		apiKey := apiKeyValue.(string)
		redisKey := "ratelimit:" + apiKey

		count, err := rdb.Incr(c.Request.Context(), redisKey).Result()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to update rate limit counter",
			})
			c.Abort()
			return
		}

		if count == 1 {
			err = rdb.Expire(c.Request.Context(), redisKey, 60*time.Second).Err()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "failed to set rate limit expiry",
				})
				c.Abort()
				return
			}
		}

		if count > 5 {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			c.Abort()
			return
		}

		c.Set("rate_count", count)

		c.Next()
	}
}
