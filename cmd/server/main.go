package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/go-batteries/shortner/app/config"
	"github.com/go-batteries/shortner/app/db"
	"github.com/go-batteries/shortner/app/models"
	"github.com/go-batteries/shortner/app/seed"
	"github.com/go-batteries/shortner/cmd/server/controller"
	"github.com/go-batteries/slicendice"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog/log"

	"html/template"

	"github.com/bradfitz/gomemcache/memcache"
)

type EchoServer struct{}

func CreateReadDatabaseConn(ctx context.Context, keyRanges []string) *db.SqliteCoordinator[string] {
	database := db.NewSqliteCoordinator(keyRanges)

	if err := database.ConnectShards(ctx, db.DBReadOnlyMode); err != nil {
		log.Fatal().Err(err).Msg("failed to connect to databases")
	}

	if _, err := database.ConnectCoordinatorDB(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to connect to coordinator db")
	}

	shards, ok := database.GetShards()
	if !ok {
		log.Fatal().Msg("should not have failed to create shards")
	}

	var shardMapper = slicendice.Reduce(
		keyRanges,
		func(acc map[string]db.Shard[string], keyRange string, _ int) map[string]db.Shard[string] {
			for _, shard := range shards {
				if shard.ShardKey() == keyRange {
					acc[keyRange] = shard
				}
			}
			return acc
		},
		map[string]db.Shard[string]{},
	)

	database.SetPolicy(&db.KeyBasedPolicy[string]{Shards: shardMapper})

	return database

}

func CreateWriteDatabaseConn(ctx context.Context, keyRanges []string) *db.SqliteCoordinator[string] {
	database := db.NewSqliteCoordinator(keyRanges)

	if err := database.ConnectShards(ctx, db.DBReadWriteMode); err != nil {
		log.Fatal().Err(err).Msg("failed to connect to databases")
	}

	if _, err := database.ConnectCoordinatorDB(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to connect to coordinator db")
	}

	shards, ok := database.GetShards()
	if !ok {
		log.Fatal().Msg("should not have failed to create shards")
	}

	database.SetPolicy(&db.RoundRobinPolicy[string]{Shards: shards})
	return database
}

type TemplateRenderer struct {
	templates *template.Template
}

// Render method to render the templates with data
func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func (app *EchoServer) StartHTTPServer(ctx context.Context, cfg *config.AppConfig) {
	seeder := seed.RegisterUrlSeeder()
	keyRanges := seeder.Shards(5)

	keyShardedDB := CreateReadDatabaseConn(ctx, keyRanges)
	robinShardedDB := CreateWriteDatabaseConn(ctx, keyRanges)

	ctrl := controller.NewURLShortnerCtrl(
		models.NewURLRepo(keyShardedDB),
		models.NewURLRepo(robinShardedDB),
		cfg.DomainName,
	)

	port := cfg.AppPort

	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{cfg.DomainName},
		AllowCredentials: true,
		AllowHeaders: []string{
			echo.HeaderOrigin,
			echo.HeaderContentType,
			echo.HeaderAccept,
			echo.HeaderContentDisposition,
			echo.HeaderConnection,
			echo.HeaderCacheControl,
			// Access Token Headers,
		},
		ExposeHeaders: []string{
			echo.HeaderContentLength,
			echo.HeaderContentDisposition,
			echo.HeaderContentEncoding,
			echo.HeaderContentType,
			echo.HeaderCacheControl,
			echo.HeaderConnection,
			// Access Token Headers,
		},
	}))

	e.Use(middleware.StaticWithConfig(middleware.StaticConfig{
		Root:   "assets/images",
		Browse: false,
	}))

	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if strings.HasPrefix(c.Path(), "/images") {
				c.Response().Header().Set("Cache-Control", "public, max-age=3600")
			}
			return next(c)
		}
	})

	if len(cfg.CacheAddrs) > 0 {
		mc := memcache.New(cfg.CacheAddrs...)
		if mc == nil {
			log.Fatal().Msg("Failed to connect to Memcached")
		}
		rateLimitConfig := controller.RateLimitConfig{
			Limit:  100,
			Window: 60 * time.Second,
		}

		e.Use(controller.RateLimiter(mc, rateLimitConfig))
	}

	e.Renderer = &TemplateRenderer{
		templates: template.Must(template.ParseGlob("views/*.html")),
	}

	e.Static("/images", "assets/images")
	// Define the route to serve the index page
	e.GET("/", func(c echo.Context) error {
		data := map[string]interface{}{
			"APIEndpoint": cfg.DomainName,
			"DomainName":  cfg.DomainName,
		}

		return c.Render(http.StatusOK, "index.html", data)
	})

	e.GET("/:shortKey", ctrl.Get)
	e.POST("/", ctrl.Post)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: e,
	}

	go func() {
		log.Info().Str("port", port).Msg("server started at")

		err := srv.ListenAndServe()
		if err != nil {
			log.Fatal().Err(err).Msg("server failed")
		}
	}()

	appCtx := context.Background()
	ctx, stop := signal.NotifyContext(appCtx, os.Interrupt)
	defer stop()

	<-ctx.Done()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("error during server shutdown")
	}
}

func main() {
	srvr := &EchoServer{}
	ctx := context.Background()

	memcachedAddrsStr := os.Getenv("MEMCACHED_URLS")
	addresses := []string{}

	if memcachedAddrsStr != "" {
		addresses = strings.Split(memcachedAddrsStr, ",")
	}

	// Define rate limit configuration
	appPort := os.Getenv("PORT")
	if appPort == "" {
		appPort = "9091"
	}

	domain := os.Getenv("DOMAIN")
	if domain == "" {
		domain = "http://localhost:" + appPort
	}

	srvr.StartHTTPServer(ctx, &config.AppConfig{
		AppPort:    appPort,
		SeedSize:   "1M",
		DomainName: domain,
		CacheAddrs: addresses,
	})
}
