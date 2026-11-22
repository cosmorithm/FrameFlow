package feed

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID       uint64 `json:"id"`
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
}
type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
}

const cookieName = "frameflow_session"
const sessionTTL = 7 * 24 * time.Hour

func viewer(c *gin.Context) *User { raw, _ := c.Get("viewer"); u, _ := raw.(*User); return u }
func viewerID(c *gin.Context) uint64 {
	if u := viewer(c); u != nil {
		return u.ID
	}
	return 0
}
func requireUser(c *gin.Context) (*User, bool) {
	u := viewer(c)
	if u == nil {
		fail(c, 401, "请先使用邮箱登录")
		return nil, false
	}
	return u, true
}
func (s *Server) middleware(c *gin.Context) {
	if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
		if !sameOrigin(c.Request) {
			fail(c, 403, "请求来源无效")
			return
		}
		if c.FullPath() != "/api/videos" {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		}
	}
	if token, err := c.Cookie(cookieName); err == nil && len(token) == 43 {
		var u User
		sum := sha256.Sum256([]byte(token))
		err = s.db.QueryRowContext(c.Request.Context(), "SELECT u.id,u.email,u.nickname FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>UTC_TIMESTAMP(3)", sum[:]).Scan(&u.ID, &u.Email, &u.Nickname)
		if err == nil {
			c.Set("viewer", &u)
		} else if !errors.Is(err, sql.ErrNoRows) {
			internal(c, err)
			return
		}
	}
	c.Next()
}
func (s *Server) createSession(ctx context.Context, tx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, id uint64) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	sum := sha256.Sum256([]byte(token))
	_, err := tx.ExecContext(ctx, "INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)", sum[:], id, time.Now().UTC().Add(sessionTTL))
	return token, err
}
func setCookie(c *gin.Context, token string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: cookieName, Value: token, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: c.Request.TLS != nil || env("COOKIE_SECURE", "false") == "true", SameSite: http.SameSiteLaxMode})
}
func parseCredentials(c *gin.Context) (credentials, bool) {
	var req credentials
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 400, "请提交邮箱和密码")
		return req, false
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Nickname = strings.TrimSpace(req.Nickname)
	a, err := mail.ParseAddress(req.Email)
	if err != nil || a.Address != req.Email || len(req.Email) > 254 || len(req.Password) == 0 || len(req.Password) > 72 {
		fail(c, 400, "请输入有效的邮箱和密码")
		return req, false
	}
	return req, true
}
func (s *Server) register(c *gin.Context) {
	req, ok := parseCredentials(c)
	if !ok {
		return
	}
	if len(req.Password) < 8 || utf8.RuneCountInString(req.Nickname) < 1 || utf8.RuneCountInString(req.Nickname) > 64 {
		fail(c, 400, "昵称限 1 至 64 个字符，密码至少 8 字节")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		internal(c, err)
		return
	}
	ctx := c.Request.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		internal(c, err)
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "INSERT INTO users(email,password_hash,nickname) VALUES(?,?,?)", req.Email, hash, req.Nickname)
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == 1062 {
			fail(c, 409, "这个邮箱已注册")
			return
		}
		internal(c, err)
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		internal(c, err)
		return
	}
	token, err := s.createSession(ctx, tx, uint64(id))
	if err != nil {
		internal(c, err)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(c, err)
		return
	}
	setCookie(c, token, int(sessionTTL.Seconds()))
	c.JSON(201, gin.H{"user": User{uint64(id), req.Email, req.Nickname}})
}
func (s *Server) login(c *gin.Context) {
	req, ok := parseCredentials(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	var u User
	var hash string
	err := s.db.QueryRowContext(ctx, "SELECT id,email,nickname,password_hash FROM users WHERE email=?", req.Email).Scan(&u.ID, &u.Email, &u.Nickname, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		internal(c, err)
		return
	}
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		fail(c, 401, "邮箱或密码错误")
		return
	}
	token, err := s.createSession(ctx, s.db, u.ID)
	if err != nil {
		internal(c, err)
		return
	}
	setCookie(c, token, int(sessionTTL.Seconds()))
	c.JSON(200, gin.H{"user": u})
}
func (s *Server) logout(c *gin.Context) {
	if token, err := c.Cookie(cookieName); err == nil {
		sum := sha256.Sum256([]byte(token))
		if _, err := s.db.ExecContext(c.Request.Context(), "DELETE FROM sessions WHERE token_hash=?", sum[:]); err != nil {
			internal(c, err)
			return
		}
	}
	setCookie(c, "", -1)
	c.Status(204)
}
