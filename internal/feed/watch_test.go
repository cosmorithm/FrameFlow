package feed

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestPlaybackTicketBindingAndCutoff(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	s := Server{loc: loc, secret: []byte("integration-test-signing-key-32-bytes")}
	stamp := time.Date(2026, 11, 22, 23, 59, 58, 0, loc)
	token, err := s.playToken(7, 42, stamp)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s.verifyToken(token, 7, 42)
	if !ok || !got.Equal(stamp) {
		t.Fatalf("unexpected ticket timestamp: %s, %v", got, ok)
	}
	if _, ok = s.verifyToken(token, 8, 42); ok {
		t.Fatal("ticket accepted for different user")
	}
	if _, ok = s.verifyToken(token, 7, 43); ok {
		t.Fatal("ticket accepted for different video")
	}
	if s.earliestDay(time.Date(2026, 11, 23, 1, 59, 59, 0, loc)) != "2026-11-22" {
		t.Fatal("previous day closed too soon")
	}
	if s.earliestDay(time.Date(2026, 11, 23, 2, 0, 0, 0, loc)) != "2026-11-23" {
		t.Fatal("previous day still open after cutoff")
	}
}

// Set FEED_TEST_DSN to an initialized development schema to exercise real InnoDB locks.
func TestConcurrentDailyTransitions(t *testing.T) {
	dsn := os.Getenv("FEED_TEST_DSN")
	if dsn == "" {
		t.Skip("set FEED_TEST_DSN to run MySQL regression")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := db.ExecContext(ctx, "INSERT INTO users(email,password_hash,nickname) VALUES(?, 'test-only', 'watch-test')", fmt.Sprintf("watch-%d@example.invalid", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	user, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM users WHERE id=?", user)
	res, err = db.ExecContext(ctx, "INSERT INTO videos(author_id,caption,video_key,status,published_at) VALUES(?, 'test-only', 'watch-test.mp4', 'published',UTC_TIMESTAMP(3))", user)
	if err != nil {
		t.Fatal(err)
	}
	video, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM videos WHERE video_id=?", video)
	s := Server{db: db, loc: time.UTC}
	start := time.Now()
	var workers sync.WaitGroup
	failures := make(chan error, 16)
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func(stage int) {
			defer workers.Done()
			failures <- s.advance(ctx, uint64(user), uint64(video), start, stage)
		}(1 + i%2)
	}
	workers.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	var rows, stage, views, qualified int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*),MAX(watch_stage) FROM video_watch_daily WHERE video_id=? AND user_id=?", video, user).Scan(&rows, &stage); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, "SELECT view_count,view_5s_count FROM videos WHERE video_id=?", video).Scan(&views, &qualified); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || stage != 2 || views != 1 || qualified != 1 {
		t.Fatalf("daily state rows=%d stage=%d counters=(%d,%d)", rows, stage, views, qualified)
	}
}
