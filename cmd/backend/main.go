package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	router := gin.Default()

	router.GET("/hello", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "hello from dummy backend",
		})
	})

	router.GET("/data", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"service": "dummy-backend",
			"status":  "ok",
		})
	})

	router.POST("/echo", func(c *gin.Context) {
		var body map[string]any

		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid json body",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"received": body,
		})
	})

	router.Run(":8081")
}
