package feed

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

var errExpiredPlay = errors.New("expired playback report")

// A playback ticket binds an actual start time to a viewer, a video and a nonce.
// All instances serving a client must use the same configured PLAY_SECRET.
func (s *Server) playToken(uid, vid uint64, now time.Time) (string, error) {
	raw := make([]byte, 40)
	binary.BigEndian.PutUint64(raw[0:8], uid)
	binary.BigEndian.PutUint64(raw[8:16], vid)
	binary.BigEndian.PutUint64(raw[16:24], uint64(now.UnixMilli()))
	if _, err := rand.Read(raw[24:]); err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(raw)
	payload := append(raw, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(payload), nil
}
func (s *Server) verifyToken(token string, uid, vid uint64) (time.Time, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 72 {
		return time.Time{}, false
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(raw[:40])
	if !hmac.Equal(mac.Sum(nil), raw[40:]) || binary.BigEndian.Uint64(raw[:8]) != uid || binary.BigEndian.Uint64(raw[8:16]) != vid {
		return time.Time{}, false
	}
	return time.UnixMilli(int64(binary.BigEndian.Uint64(raw[16:24]))).UTC(), true
}

// Today's date is always open; yesterday remains open through 01:59:59 local time.
func (s *Server) earliestDay(now time.Time) string {
	local := now.In(s.loc)
	if local.Hour() < 2 {
		local = local.AddDate(0, 0, -1)
	}
	return local.Format(time.DateOnly)
}
func (s *Server) advance(ctx context.Context, uid, vid uint64, start time.Time, stage int) error {
	day := start.In(s.loc).Format(time.DateOnly)
	if day < s.earliestDay(time.Now()) {
		return errExpiredPlay
	}
	// Stages only increase. Replayed events already covered by an open day's row
	// do not need to contend on the video's aggregate counter.
	var old int
	err := s.db.QueryRowContext(ctx, "SELECT watch_stage FROM video_watch_daily WHERE stat_date=? AND video_id=? AND user_id=?", day, vid, uid).Scan(&old)
	if err == nil && old >= stage {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Consistent locking order also avoids two inserts upgrading FK read locks on
	// the same videos row at the same time.
	var found uint64
	err = tx.QueryRowContext(ctx, "SELECT video_id FROM videos WHERE video_id=? AND status='published' FOR UPDATE", vid).Scan(&found)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO video_watch_daily(stat_date,video_id,user_id,watch_stage) VALUES(?,?,?,0) ON DUPLICATE KEY UPDATE watch_stage=watch_stage", day, vid, uid)
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, "SELECT watch_stage FROM video_watch_daily WHERE stat_date=? AND video_id=? AND user_id=? FOR UPDATE", day, vid, uid).Scan(&old)
	if err != nil {
		return err
	}
	if day < s.earliestDay(time.Now()) {
		return errExpiredPlay
	}
	if old >= stage {
		return tx.Commit()
	}
	views, qualified := 0, 0
	if old < 1 {
		views = 1
	}
	if stage == 2 {
		qualified = 1
	}
	if _, err = tx.ExecContext(ctx, "UPDATE video_watch_daily SET watch_stage=? WHERE stat_date=? AND video_id=? AND user_id=?", stage, day, vid, uid); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE videos SET view_count=view_count+?,view_5s_count=view_5s_count+? WHERE video_id=?", views, qualified, vid); err != nil {
		return err
	}
	return tx.Commit()
}
func watchError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errExpiredPlay):
		fail(c, 410, "该播放已超过补报窗口")
	case errors.Is(err, sql.ErrNoRows):
		fail(c, 404, "视频不存在或尚未发布")
	default:
		internal(c, err)
	}
}
func (s *Server) startPlay(c *gin.Context) {
	u, ok := requireUser(c)
	if !ok {
		return
	}
	vid, ok := idParam(c)
	if !ok || !s.videoExists(c, vid) {
		return
	}
	start := time.Now().UTC()
	token, err := s.playToken(u.ID, vid, start)
	if err != nil {
		internal(c, err)
		return
	}
	if err = s.advance(c.Request.Context(), u.ID, vid, start, 1); err != nil {
		watchError(c, err)
		return
	}
	c.JSON(201, gin.H{"play_id": token})
}
func (s *Server) fiveSeconds(c *gin.Context) {
	u, ok := requireUser(c)
	if !ok {
		return
	}
	vid, ok := idParam(c)
	if !ok || !s.videoExists(c, vid) {
		return
	}
	start, ok := s.verifyToken(c.Param("playID"), u.ID, vid)
	if !ok {
		fail(c, 400, "播放凭据无效")
		return
	}
	if time.Since(start) < 5*time.Second {
		fail(c, 409, "播放尚未达到五秒")
		return
	}
	if err := s.advance(c.Request.Context(), u.ID, vid, start, 2); err != nil {
		watchError(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) cleanup(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		day := s.earliestDay(time.Now())
		err := s.purge(ctx, "DELETE FROM video_watch_daily WHERE stat_date<? LIMIT 500", day)
		if err == nil {
			err = s.purge(ctx, "DELETE FROM sessions WHERE expires_at < UTC_TIMESTAMP(3) LIMIT 500")
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("cleanup: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Server) purge(ctx context.Context, query string, args ...any) error {
	for {
		res, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n < 500 {
			return err
		}
	}
}
