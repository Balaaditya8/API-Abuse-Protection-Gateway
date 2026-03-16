package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
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

	router.Any("/api/*proxyPath", APIKeyAuthMiddleware(dbpool),
		BlockCheckMiddleware(rdb),
		RateLimitMiddleware(rdb),
		ForwardHandler("http://localhost:8081"))

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

func forwardHelloHandler(c *gin.Context) {
	resp, err := http.Get("http://localhost:8081/hello")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": "failed to reach backend",
		})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to read backend response",
		})
		return
	}

	c.Data(resp.StatusCode, "application/json", body)
}

func ForwardHandler(backendBaseURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		targetURL := backendBaseURL + c.Param("proxyPath")
		if c.Request.URL.RawQuery != "" {
			targetURL += "?" + c.Request.URL.RawQuery
		}

		req, err := http.NewRequest(
			c.Request.Method,
			targetURL,
			c.Request.Body,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to create backend request",
			})
			return
		}

		// copy headers from original request
		for key, values := range c.Request.Header {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{
				"error": "failed to reach backend service",
			})
			return
		}
		defer resp.Body.Close()

		// copy backend response headers
		for key, values := range resp.Header {
			for _, value := range values {
				c.Writer.Header().Add(key, value)
			}
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to read backend response",
			})
			return
		}

		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
}
