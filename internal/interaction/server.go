// Package interaction implements the private gRPC service and owns the interaction schema.
package interaction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"

	pb "github.com/cosmorithm/FrameFlow/proto/interaction/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedInteractionServiceServer
	db *sql.DB
}

func New(db *sql.DB) *Server { return &Server{db: db} }

func databaseError(ctx context.Context, err error) error {
	if cause := ctx.Err(); cause != nil {
		return status.FromContextError(cause).Err()
	}
	log.Printf("interaction database operation failed: %v", err)
	return status.Error(codes.Internal, "互动数据暂时不可用")
}

func validateMutation(req *pb.MutationRequest) error {
	if req.GetUserId() == 0 || req.GetTargetId() == 0 {
		return status.Error(codes.InvalidArgument, "用户和目标 ID 必须为正数")
	}
	return nil
}

const batchSQL = `
SELECT ref.video_id,
       (SELECT COUNT(*) FROM video_likes l WHERE l.video_id = ref.video_id),
       (SELECT COUNT(*) FROM video_comments c WHERE c.video_id = ref.video_id AND c.deleted_at IS NULL),
       EXISTS(SELECT 1 FROM video_likes l WHERE l.user_id = ? AND l.video_id = ref.video_id),
       EXISTS(SELECT 1 FROM video_favorites f WHERE f.user_id = ? AND f.video_id = ref.video_id),
       EXISTS(SELECT 1 FROM user_follows f WHERE f.follower_id = ? AND f.followee_id = ref.author_id)
FROM JSON_TABLE(?, '$[*]' COLUMNS (
    pos FOR ORDINALITY,
    video_id BIGINT UNSIGNED PATH '$.video_id',
    author_id BIGINT UNSIGNED PATH '$.author_id'
)) AS ref
ORDER BY ref.pos`

func (s *Server) BatchGetInteractions(ctx context.Context, req *pb.BatchRequest) (*pb.BatchResponse, error) {
	out := &pb.BatchResponse{}
	if len(req.GetVideos()) > 100 {
		return nil, status.Error(codes.InvalidArgument, "每页最多 100 条视频")
	}
	if len(req.GetVideos()) == 0 {
		return out, nil
	}
	for _, ref := range req.Videos {
		if ref.GetVideoId() == 0 || ref.GetAuthorId() == 0 {
			return nil, status.Error(codes.InvalidArgument, "视频与作者 ID 必须为正数")
		}
	}
	// Generated VideoRef fields have stable snake_case JSON names. Marshal their uint64s
	// directly for JSON_TABLE so IDs never pass through floating point.
	payload, err := json.Marshal(req.Videos)
	if err != nil {
		return nil, status.Error(codes.Internal, "无法编码视频列表")
	}
	rows, err := s.db.QueryContext(ctx, batchSQL, req.ViewerId, req.ViewerId, req.ViewerId, string(payload))
	if err != nil {
		return nil, databaseError(ctx, err)
	}
	defer rows.Close()
	for rows.Next() {
		item := &pb.Interaction{}
		if err := rows.Scan(&item.VideoId, &item.LikeCount, &item.CommentCount, &item.Liked, &item.Favorited, &item.Following); err != nil {
			return nil, databaseError(ctx, err)
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError(ctx, err)
	}
	return out, nil
}

// The identifiers are compile-time constants selected by the RPC method. Explicitly
// setting the target state preserves created_at on retries and reorders no favorites.
type relation struct{ table, owner, target string }

var (
	videoLike   = relation{"video_likes", "user_id", "video_id"}
	favorite    = relation{"video_favorites", "user_id", "video_id"}
	follow      = relation{"user_follows", "follower_id", "followee_id"}
	commentLike = relation{"comment_likes", "user_id", "comment_id"}
)

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func applyRelation(ctx context.Context, db executor, r relation, req *pb.MutationRequest) (*pb.MutationResponse, error) {
	if err := validateMutation(req); err != nil {
		return nil, err
	}
	query := "DELETE FROM " + r.table + " WHERE " + r.owner + "=? AND " + r.target + "=?"
	if req.Enabled {
		query = "INSERT INTO " + r.table + " (" + r.owner + ", " + r.target + ") VALUES (?, ?) ON DUPLICATE KEY UPDATE created_at = created_at"
	}
	if _, err := db.ExecContext(ctx, query, req.UserId, req.TargetId); err != nil {
		return nil, databaseError(ctx, err)
	}
	return &pb.MutationResponse{Enabled: req.Enabled}, nil
}
func (s *Server) SetLike(ctx context.Context, req *pb.MutationRequest) (*pb.MutationResponse, error) {
	return applyRelation(ctx, s.db, videoLike, req)
}
func (s *Server) SetFavorite(ctx context.Context, req *pb.MutationRequest) (*pb.MutationResponse, error) {
	return applyRelation(ctx, s.db, favorite, req)
}
func (s *Server) SetFollow(ctx context.Context, req *pb.MutationRequest) (*pb.MutationResponse, error) {
	if req.GetUserId() == req.GetTargetId() && req.GetEnabled() {
		return nil, status.Error(codes.InvalidArgument, "不能关注自己")
	}
	return applyRelation(ctx, s.db, follow, req)
}
func (s *Server) SetCommentLike(ctx context.Context, req *pb.MutationRequest) (*pb.MutationResponse, error) {
	if err := validateMutation(req); err != nil {
		return nil, err
	}
	if !req.Enabled {
		return applyRelation(ctx, s.db, commentLike, req)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, databaseError(ctx, err)
	}
	defer tx.Rollback()
	var id uint64
	err = tx.QueryRowContext(ctx, "SELECT comment_id FROM video_comments WHERE comment_id=? AND deleted_at IS NULL FOR SHARE", req.TargetId).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "评论不存在或已删除")
	}
	if err != nil {
		return nil, databaseError(ctx, err)
	}
	out, err := applyRelation(ctx, tx, commentLike, req)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, databaseError(ctx, err)
	}
	return out, nil
}
