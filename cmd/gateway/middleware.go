package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func APIKeyAuthMiddleware(dbpool *pgxpool.Pool, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {

		apiKey := c.GetHeader("x-api-key")
		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "missing api key",
			})
			c.Abort()
			return
		}
		ip := c.ClientIP()
		invalidKey := "invalidkey:" + ip
		query := `
			SELECT t.id, k.active, t.rate_limit_count, t.rate_limit_window_sec
			FROM api_keys k
			JOIN tenants t ON k.tenant_id = t.id
			WHERE k.key = $1
		`

		var tenantID int64
		var active bool
		var rateLimitCount int64
		var rateLimitWindow int64

		err := dbpool.QueryRow(
			c.Request.Context(),
			query,
			apiKey,
		).Scan(&tenantID, &active, &rateLimitCount, &rateLimitWindow)

		if err != nil {
			invalidCount, _ := rdb.Incr(c.Request.Context(), invalidKey).Result()

			if invalidCount == 1 {
				err = rdb.Expire(c.Request.Context(), invalidKey, 5*time.Minute).Err()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"error": "failed to set invalid limit expiry",
					})
					c.Abort()
					return
				}
			}

			if invalidCount > 10 {
				blockedIP := "blockedIP:" + ip
				err = rdb.Set(c.Request.Context(), blockedIP, "1", 10*time.Minute).Err()

				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"error": "failed to block ip",
					})
					c.Abort()
					return
				}
			}

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
		c.Set("rate_limit_count", rateLimitCount)
		c.Set("rate_limit_window", rateLimitWindow)
		fmt.Println("tenantID:", tenantID)
		fmt.Println("rateLimitCount:", rateLimitCount)
		fmt.Println("rateLimitWindow:", rateLimitWindow)
		c.Next()
	}
}

func RateLimitMiddleware(rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKeyValue, exists := c.Get("api_key")
		limitValue, exists := c.Get("rate_limit_count")
		windowValue, exists := c.Get("rate_limit_window")
		if !exists {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "api key missing from context",
			})
			c.Abort()
			return
		}

		apiKey := apiKeyValue.(string)
		limit := limitValue.(int64)
		window := windowValue.(int64)
		redisKey := "ratelimit:" + apiKey
		violationKey := "violations:" + apiKey

		count, err := rdb.Incr(c.Request.Context(), redisKey).Result()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to update rate limit counter",
			})
			c.Abort()
			return
		}

		if count == 1 {
			err = rdb.Expire(c.Request.Context(), redisKey, time.Duration(window)*time.Second).Err()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "failed to set rate limit expiry",
				})
				c.Abort()
				return
			}
		}

		if count > limit {
			violationCount, err := rdb.Incr(c.Request.Context(), violationKey).Result()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "failed to update violation counter",
				})
				c.Abort()
				return
			}

			if violationCount == 1 {
				err = rdb.Expire(c.Request.Context(), violationKey, 5*time.Minute).Err()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"error": "failed to set violation expiry",
					})
					c.Abort()
					return
				}
			}

			if violationCount >= 3 {
				blockKey := "blocked:" + apiKey

				err = rdb.Set(c.Request.Context(), blockKey, "1", 10*time.Minute).Err()

				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"error": "failed to block api key",
					})
					c.Abort()
					return
				}
			}

			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":           "rate limit exceeded",
				"violation_count": violationCount,
			})
			c.Abort()
			return
		}

		c.Set("rate_count", count)

		c.Next()
	}
}

func BlockCheckMiddleware(rdb *redis.Client) gin.HandlerFunc {
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
		blockKey := "blocked:" + apiKey

		_, err := rdb.Get(c.Request.Context(), blockKey).Result()

		if err == redis.Nil {
			c.Next()
			return
		}

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to check block status",
			})
			c.Abort()
			return
		}

		c.JSON(http.StatusForbidden, gin.H{
			"error": "api key temporarily blocked",
		})
		c.Abort()
	}

}

func IPBlockerMiddleware(rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		blockedIP := "blockedIP:" + ip
		_, err := rdb.Get(c.Request.Context(), blockedIP).Result()

		if err == redis.Nil {
			c.Next()
			return
		}

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to check block status",
			})
			c.Abort()
			return
		}

		c.JSON(http.StatusForbidden, gin.H{
			"error": "IPtemporarily blocked",
		})
		c.Abort()

	}
}
