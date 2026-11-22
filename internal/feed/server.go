package feed

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	pb "github.com/cosmorithm/FrameFlow/proto/interaction/v1"
	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Server struct {
	db          *sql.DB
	redis       *redis.Client
	interaction pb.InteractionServiceClient
	secret      []byte
	loc         *time.Location
	uploadDir   string
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func Run(ctx context.Context) error {
	cfg, err := mysql.ParseDSN(env("FEED_DSN", "root@tcp(127.0.0.1:3306)/frameflow_proto_feed"))
	if err != nil {
		return fmt.Errorf("FEED_DSN: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(5 * time.Minute)
	startup, stop := context.WithTimeout(ctx, 5*time.Second)
	err = db.PingContext(startup)
	stop()
	if err != nil {
		return fmt.Errorf("connect to feed MySQL: %w", err)
	}
	conn, err := grpc.NewClient(env("INTERACTION_ADDR", "127.0.0.1:9091"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	loc, err := time.LoadLocation(env("STAT_TIMEZONE", "Asia/Shanghai"))
	if err != nil {
		return err
	}
	secret := []byte(os.Getenv("PLAY_SECRET"))
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err = rand.Read(secret); err != nil {
			return err
		}
	} else if len(secret) < 32 {
		return errors.New("PLAY_SECRET must contain at least 32 bytes")
	}
	s := Server{db: db, interaction: pb.NewInteractionServiceClient(conn), secret: secret, loc: loc, uploadDir: env("UPLOAD_DIR", "data/uploads")}
	if err = os.MkdirAll(s.uploadDir, 0700); err != nil {
		return err
	}
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		s.redis = redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD"), DialTimeout: 250 * time.Millisecond, ReadTimeout: 250 * time.Millisecond, WriteTimeout: 250 * time.Millisecond, MaxRetries: -1})
		defer s.redis.Close()
	}
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.SetTrustedProxies(nil)
	api := r.Group("/api")
	api.Use(s.middleware)
	api.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	api.GET("/me", func(c *gin.Context) { c.JSON(200, gin.H{"user": viewer(c)}) })
	api.POST("/register", s.register)
	api.POST("/login", s.login)
	api.POST("/logout", s.logout)
	api.GET("/feed", s.feed)
	api.GET("/favorites", s.collection)
	api.GET("/likes", s.collection)
	api.POST("/videos", s.publish)
	api.PUT("/videos/:id/like", s.mutate)
	api.PUT("/videos/:id/favorite", s.mutate)
	api.PUT("/users/:id/follow", s.mutate)
	api.PUT("/comments/:id/like", s.mutate)
	api.GET("/videos/:id/comments", s.comments)
	api.POST("/videos/:id/comments", s.addComment)
	api.POST("/videos/:id/plays", s.startPlay)
	api.POST("/videos/:id/plays/:playID/five-seconds", s.fiveSeconds)
	r.StaticFS("/media", gin.Dir(env("MEDIA_DIR", "media"), false))
	r.StaticFS("/uploads", gin.Dir(s.uploadDir, false))
	dist := env("WEB_DIR", "web/dist")
	r.StaticFS("/assets", gin.Dir(filepath.Join(dist, "assets"), false))
	r.GET("/", func(c *gin.Context) { c.File(filepath.Join(dist, "index.html")) })
	httpServer := http.Server{Addr: env("LISTEN_ADDR", "127.0.0.1:8080"), Handler: r, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second}
	go s.cleanup(ctx)
	go func() {
		<-ctx.Done()
		closing, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(closing)
	}()
	log.Printf("Feed API at http://%s", httpServer.Addr)
	err = httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func fail(c *gin.Context, code int, message string) {
	c.AbortWithStatusJSON(code, gin.H{"error": message})
}
func internal(c *gin.Context, err error) {
	log.Printf("%s %s: %v", c.Request.Method, c.FullPath(), err)
	fail(c, 500, "服务暂不可用")
}
func idParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, 400, "无效的资源 ID")
		return 0, false
	}
	return id, true
}
func limitParam(c *gin.Context, def int) (int, bool) {
	value := c.Query("limit")
	if value == "" {
		return def, true
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > 50 {
		fail(c, 400, "limit 必须是 1 至 50")
		return 0, false
	}
	return n, true
}
func sameOrigin(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	raw := r.Header.Get("Origin")
	if raw == "" {
		return true
	}
	origin, err := url.Parse(raw)
	return err == nil && (origin.Scheme == "http" || origin.Scheme == "https") && strings.EqualFold(origin.Host, r.Host)
}
func exists(ctx context.Context, db *sql.DB, query string, id uint64) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, query, id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
func (s *Server) videoExists(c *gin.Context, id uint64) bool {
	ok, err := exists(c.Request.Context(), s.db, "SELECT 1 FROM videos WHERE video_id=? AND status='published'", id)
	if err != nil {
		internal(c, err)
		return false
	}
	if !ok {
		fail(c, 404, "视频不存在或尚未发布")
	}
	return ok
}

const pageCacheKey = "frameflow:feed:latest:first:8"
