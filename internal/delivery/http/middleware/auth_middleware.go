package middleware

import (
	"go-nexus/pkg/auth"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var jwtSecret = []byte("my-jwt-secret")

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header missing"})
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization format"})
			return
		}
		tokenString := parts[1]
		userID, err := auth.ParseToken(tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token or Invalid claims"})
			return
		}
		c.Set("userID", userID)
		c.Next()
	}
}
