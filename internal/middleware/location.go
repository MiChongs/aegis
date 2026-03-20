package middleware

import (
	"context"
	"strings"

	"aegis/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	clientIPContextKey   = "request.client_ip"
	locationContextKey   = "request.ip_location"
	locationTextFallback = ""
)

func Location(locationService *service.LocationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		c.Set(clientIPContextKey, clientIP)
		if locationService == nil {
			c.Set(locationContextKey, service.IPLocation{IP: clientIP, Location: locationTextFallback, Source: "default"})
			c.Next()
			return
		}

		location := locationService.DefaultLocation(clientIP)
		cacheCtx, cancel := context.WithTimeout(c.Request.Context(), locationService.CacheLookupTimeout())
		if cached, ok := locationService.GetCached(cacheCtx, clientIP); ok {
			location = cached
		} else {
			locationService.RefreshAsync(clientIP)
		}
		cancel()

		c.Set(locationContextKey, location)
		c.Next()
	}
}

func RequestLocation(c *gin.Context) service.IPLocation {
	if value, ok := c.Get(locationContextKey); ok {
		if location, ok := value.(service.IPLocation); ok {
			return location
		}
	}
	return service.IPLocation{}
}

func RequestLocationString(c *gin.Context) string {
	location := RequestLocation(c)
	if text := strings.TrimSpace(location.Location); text != "" {
		return text
	}
	parts := make([]string, 0, 4)
	for _, part := range []string{location.Country, location.Region, location.City, location.District} {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " ")
}

func RequestClientIP(c *gin.Context) string {
	if value, ok := c.Get(clientIPContextKey); ok {
		if ip, ok := value.(string); ok {
			return ip
		}
	}
	return c.ClientIP()
}
