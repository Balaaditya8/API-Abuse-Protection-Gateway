package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Tenant struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type APIKey struct {
	ID        int64     `json:"id"`
	TenantID  int64     `json:"tenant_id"`
	Key       string    `json:"key"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateTenantRequest struct {
	Name string `json:"name"`
}

func main() {

	dbURL := "postgres://postgres:postgres@localhost:5432/rate_limiter"
	if envURL := os.Getenv("DATABASE_URL"); envURL != "" {
		dbURL = envURL
	}

	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   0,
	})

	pong, err := rdb.Ping(ctx).Result()
	if err != nil {
		panic(err)
	}

	fmt.Println("Redis connected:", pong)

	dbpool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		panic(err)
	}
	defer dbpool.Close()

	router := gin.Default()

	router.POST("/tenants", func(c *gin.Context) {
		var req CreateTenantRequest

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid JSON Body",
			})
			return
		}

		if req.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "name is required",
			})
			return
		}

		var tenant Tenant

		query := `
		INSERT INTO tenants (name)
		VALUES ($1)
		RETURNING id, name, created_at
		`

		err := dbpool.QueryRow(ctx, query, req.Name).Scan(
			&tenant.ID,
			&tenant.Name,
			&tenant.CreatedAt,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			return
		}
		c.JSON(http.StatusCreated, tenant)
	})

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Server is Running",
		})
	})

	router.POST("/tenants/:id/keys", func(c *gin.Context) {
		tenantIdStr := c.Param("id")
		tenantId, err := strconv.ParseInt(tenantIdStr, 10, 64)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			return
		}

		query := `
		SELECT id FROM tenants WHERE id = $1
		`

		var tenant Tenant

		err = dbpool.QueryRow(ctx, query, tenantId).Scan(
			&tenant.ID,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			return
		}

		generatedKey, err := generateAPIKey()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to generate api key",
			})
			return
		}

		var apiKey APIKey

		query = `
		INSERT INTO api_keys (tenant_id, key) 
		VALUES ($1,$2)
		RETURNING id, tenant_id, key, active, created_at
		`

		err = dbpool.QueryRow(ctx, query, tenantId, generatedKey).Scan(
			&apiKey.ID,
			&apiKey.TenantID,
			&apiKey.Key,
			&apiKey.Active,
			&apiKey.CreatedAt,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Could not add",
			})
			return
		}
		c.JSON(http.StatusCreated, gin.H{
			"api_key": generatedKey,
		})

	})

	router.GET("/protected", func(c *gin.Context) {

		apiKey := c.GetHeader("x-api-key")
		if apiKey == "" {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Empty api key",
			})
			return
		}

		query := `
		SELECT tenant_id, active from api_keys where key=$1
		`

		var tenant_id int64
		var active bool

		err := dbpool.QueryRow(ctx, query, apiKey).Scan(
			&tenant_id,
			&active,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Api Key Not Found",
			})
			return
		}

		if !active {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "api key inactive",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message":   "valid api key",
			"tenant_id": tenant_id,
		})

		redisKey := "ratelimit:" + apiKey

		count, err := rdb.Incr(ctx, redisKey).Result()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to update rate limit counter",
			})
			return
		}

		if count == 1 {
			err = rdb.Expire(ctx, redisKey, 60*time.Second).Err()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "failed to set rate limit expiry",
				})
				return
			}
		}

		if count > 5 {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"count": count,
		})
	})

	if err := router.Run(":8080"); err != nil {
		panic(err)
	}
}

func generateAPIKey() (string, error) {
	bytes := make([]byte, 16)

	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}

	return "rk_" + hex.EncodeToString(bytes), nil
}
