package interaction

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	pb "github.com/cosmorithm/FrameFlow/proto/interaction/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) CreateComment(ctx context.Context, req *pb.CreateCommentRequest) (*pb.Comment, error) {
	content := strings.TrimSpace(req.GetContent())
	if req.GetUserId() == 0 || req.GetVideoId() == 0 || content == "" || utf8.RuneCountInString(content) > 1000 {
		return nil, status.Error(codes.InvalidArgument, "评论需要用户、视频和 1 至 1000 个字符的正文")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, databaseError(ctx, err)
	}
	defer tx.Rollback()
	var parent, root any
	var rootID uint64
	if req.ParentId != 0 {
		// The video predicate prevents replies to a comment on an unrelated video.
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(root_id,comment_id) FROM video_comments
			WHERE video_id=? AND comment_id=? AND deleted_at IS NULL FOR SHARE`, req.VideoId, req.ParentId).Scan(&rootID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "被回复的评论不存在或已删除")
		}
		if err != nil {
			return nil, databaseError(ctx, err)
		}
		parent, root = req.ParentId, rootID
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	result, err := tx.ExecContext(ctx, `INSERT INTO video_comments (video_id,author_id,parent_id,root_id,content,created_at)
		VALUES(?,?,?,?,?,?)`, req.VideoId, req.UserId, parent, root, content, at)
	if err != nil {
		return nil, databaseError(ctx, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, databaseError(ctx, err)
	}
	if err = tx.Commit(); err != nil {
		return nil, databaseError(ctx, err)
	}
	return &pb.Comment{CommentId: uint64(id), VideoId: req.VideoId, AuthorId: req.UserId, ParentId: req.ParentId,
		RootId: rootID, Content: content, CreatedAt: at.Format(time.RFC3339Nano)}, nil
}

const commentSelect = `SELECT c.comment_id,c.video_id,c.author_id,COALESCE(c.parent_id,0),COALESCE(c.root_id,0),
    IF(c.deleted_at IS NULL,c.content,'该评论已删除'),c.created_at,c.deleted_at IS NOT NULL,
    (SELECT COUNT(*) FROM comment_likes l WHERE l.comment_id=c.comment_id),
    EXISTS(SELECT 1 FROM comment_likes l WHERE l.user_id=? AND l.comment_id=c.comment_id),
    (SELECT COUNT(*) FROM video_comments r WHERE r.video_id=c.video_id AND r.root_id=c.comment_id AND r.deleted_at IS NULL)
FROM video_comments c WHERE c.video_id=?`

func (s *Server) ListComments(ctx context.Context, req *pb.ListCommentsRequest) (*pb.ListCommentsResponse, error) {
	if req.GetVideoId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "需要视频 ID")
	}
	cursor, n, err := parsePage(req.Cursor, req.Limit)
	if err != nil {
		return nil, err
	}
	query := commentSelect
	args := []any{req.ViewerId, req.VideoId}
	operator, sort := "<", "DESC"
	if req.RootId == 0 {
		query += " AND c.root_id IS NULL"
	} else {
		query += " AND c.root_id=?"
		args = append(args, req.RootId)
		operator, sort = ">", "ASC"
	}
	if cursor.id != 0 {
		query += " AND (c.created_at " + operator + " ? OR (c.created_at = ? AND c.comment_id " + operator + " ?))"
		args = append(args, cursor.created, cursor.created, cursor.id)
	}
	query += " ORDER BY c.created_at " + sort + ", c.comment_id " + sort + " LIMIT ?"
	rows, err := s.db.QueryContext(ctx, query, append(args, n+1)...)
	if err != nil {
		return nil, databaseError(ctx, err)
	}
	defer rows.Close()
	out := &pb.ListCommentsResponse{}
	var last pageCursor
	for rows.Next() {
		item := &pb.Comment{}
		var at time.Time
		if err := rows.Scan(&item.CommentId, &item.VideoId, &item.AuthorId, &item.ParentId, &item.RootId, &item.Content,
			&at, &item.Deleted, &item.LikeCount, &item.Liked, &item.ReplyCount); err != nil {
			return nil, databaseError(ctx, err)
		}
		if len(out.Items) == n {
			out.NextCursor = last.String()
			break
		}
		item.CreatedAt = at.UTC().Format(time.RFC3339Nano)
		out.Items = append(out.Items, item)
		last = pageCursor{created: at, id: item.CommentId}
	}
	if err = rows.Err(); err != nil {
		return nil, databaseError(ctx, err)
	}
	return out, nil
}
