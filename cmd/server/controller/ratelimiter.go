package controller

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/labstack/echo/v4"
)

type RateLimitConfig struct {
	Limit  int
	Window time.Duration
}

func incrMissings(val string, missing *int) {
	if val == "" {
		*missing += 1
	}
}

func RateLimiter(mc *memcache.Client, config RateLimitConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Extract relevant headers and IP
			host := c.Request().Header.Get("Host")
			userAgent := c.Request().Header.Get("User-Agent")
			uniqueID := c.Request().Header.Get("UniqueID") // Custom header
			authKey := c.Request().Header.Get("AuthKey")
			ip := c.RealIP()

			missings := 0
			incrMissings(host, &missings)
			incrMissings(userAgent, &missings)
			incrMissings(uniqueID, &missings)
			incrMissings(authKey, &missings)
			incrMissings(ip, &missings)

			if missings > 3 {
				return c.JSON(http.StatusForbidden, `{"apologies": "you doing it wrong"}`)
			}

			rateKey := fmt.Sprintf("rate:%s:%s:%s:%s:%s", host, userAgent, uniqueID, authKey, ip)

			// Check the request count in Memcached
			item, err := mc.Get(rateKey)
			var count int
			if err == nil {
				// Parse existing count
				fmt.Sscanf(string(item.Value), "%d", &count)
			}

			// Block if the limit is exceeded
			if count >= config.Limit {
				return c.JSON(http.StatusTooManyRequests, map[string]string{
					"message": "Rate limit exceeded. Please try again later.",
				})
			}

			// Increment the request count and set expiration if it's the first request
			count++
			err = mc.Set(&memcache.Item{
				Key:        rateKey,
				Value:      []byte(fmt.Sprintf("%d", count)),
				Expiration: int32(config.Window.Seconds()),
			})
			if err != nil {
				log.Println("Memcached error:", err)
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"message": "Internal server error.",
				})
			}

			// Proceed to the next handler
			return next(c)
		}
	}
}
