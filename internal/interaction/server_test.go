package interaction

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	pb "github.com/cosmorithm/FrameFlow/proto/interaction/v1"
	"github.com/go-sql-driver/mysql"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPageCursorRoundTrip(t *testing.T) {
	want := pageCursor{created: time.Date(2026, 11, 22, 12, 30, 2, 123000000, time.UTC), id: 4201}
	got, limit, err := parsePage(want.String(), 0)
	if err != nil || limit != 20 || got != want {
		t.Fatalf("round trip = %+v, %d, %v", got, limit, err)
	}
	for _, value := range []string{"1", "-1.1", "1.0", "999999999999999.1", "x.1"} {
		if _, _, err := parsePage(value, 1); status.Code(err) != codes.InvalidArgument {
			t.Errorf("accepted cursor %q", value)
		}
	}
}

// Set FRAMEFLOW_TEST_INTERACTION_DSN to a schema initialized with db/interaction.sql.
// Each run creates isolated records and removes just those records when it finishes.
func TestInteractionMySQL(t *testing.T) {
	dsn := os.Getenv("FRAMEFLOW_TEST_INTERACTION_DSN")
	if dsn == "" {
		t.Skip("set FRAMEFLOW_TEST_INTERACTION_DSN for MySQL integration coverage")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid integration DSN")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { db.Close() })
	s := New(db)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	base := uint64(time.Now().UnixNano())
	user, video, author := base, base+1, base+2
	t.Cleanup(func() {
		for _, q := range []string{
			"DELETE l FROM comment_likes l JOIN video_comments c ON c.comment_id=l.comment_id WHERE c.video_id=?",
			"DELETE FROM video_comments WHERE video_id=?",
			"DELETE FROM video_likes WHERE video_id=?",
		} {
			if _, err := db.Exec(q, video); err != nil {
				t.Error(err)
			}
		}
	})

	set := &pb.MutationRequest{UserId: user, TargetId: video, Enabled: true}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.SetLike(ctx, set); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	root, err := s.CreateComment(ctx, &pb.CreateCommentRequest{UserId: user, VideoId: video, Content: "一级评论"})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := s.CreateComment(ctx, &pb.CreateCommentRequest{UserId: author, VideoId: video, ParentId: root.CommentId, Content: "回复"})
	if err != nil {
		t.Fatal(err)
	}
	nested, err := s.CreateComment(ctx, &pb.CreateCommentRequest{UserId: user, VideoId: video, ParentId: reply.CommentId, Content: "回复的回复"})
	if err != nil || nested.GetRootId() != root.CommentId || nested.GetParentId() != reply.CommentId {
		t.Fatalf("nested = %v, %v", nested, err)
	}
	if _, err = s.CreateComment(ctx, &pb.CreateCommentRequest{UserId: user, VideoId: video + 1, ParentId: root.CommentId, Content: "跨视频"}); status.Code(err) != codes.NotFound {
		t.Fatalf("cross-video reply error: %v", err)
	}
	if _, err = s.SetCommentLike(ctx, &pb.MutationRequest{UserId: user, TargetId: nested.CommentId, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListComments(ctx, &pb.ListCommentsRequest{ViewerId: user, VideoId: video, RootId: root.CommentId, Limit: 1})
	if err != nil || len(first.GetItems()) != 1 || first.Items[0].CommentId != reply.CommentId || first.NextCursor == "" {
		t.Fatalf("first = %v, %v", first, err)
	}
	second, err := s.ListComments(ctx, &pb.ListCommentsRequest{ViewerId: user, VideoId: video, RootId: root.CommentId, Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.GetItems()) != 1 || second.Items[0].CommentId != nested.CommentId || !second.Items[0].Liked || second.NextCursor != "" {
		t.Fatalf("second = %v, %v", second, err)
	}

	request := &pb.BatchRequest{ViewerId: user, Videos: []*pb.VideoRef{{VideoId: video, AuthorId: author}}}
	batch, err := s.BatchGetInteractions(ctx, request)
	if err != nil || len(batch.GetItems()) != 1 || batch.Items[0].LikeCount != 1 || !batch.Items[0].Liked || batch.Items[0].CommentCount != 3 {
		t.Fatalf("batch = %v, %v", batch, err)
	}
	request.ViewerId = author
	batch, err = s.BatchGetInteractions(ctx, request)
	if err != nil || len(batch.GetItems()) != 1 || batch.Items[0].LikeCount != 1 || batch.Items[0].Liked {
		t.Fatalf("other viewer = %v, %v", batch, err)
	}
}
