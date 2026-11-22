package interaction

import (
	"context"
	"strconv"
	"strings"
	"time"

	pb "github.com/cosmorithm/FrameFlow/proto/interaction/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type pageCursor struct {
	created time.Time
	id      uint64
}

func (p pageCursor) String() string {
	return strconv.FormatInt(p.created.UnixMilli(), 10) + "." + strconv.FormatUint(p.id, 10)
}

func parsePage(cursor string, limit uint32) (pageCursor, int, error) {
	if limit > 100 {
		return pageCursor{}, 0, status.Error(codes.InvalidArgument, "每页最多 100 条")
	}
	if limit == 0 {
		limit = 20
	}
	if cursor == "" {
		return pageCursor{}, int(limit), nil
	}
	msPart, idPart, ok := strings.Cut(cursor, ".")
	ms, msErr := strconv.ParseInt(msPart, 10, 64)
	id, idErr := strconv.ParseUint(idPart, 10, 64)
	if !ok || msErr != nil || idErr != nil || ms < 0 || ms > 253402300799999 || id == 0 {
		return pageCursor{}, 0, status.Error(codes.InvalidArgument, "无效的分页游标")
	}
	return pageCursor{created: time.UnixMilli(ms).UTC(), id: id}, int(limit), nil
}

func (s *Server) listSaved(ctx context.Context, r relation, req *pb.ListRequest) (*pb.ListResponse, error) {
	if req.GetUserId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "需要用户 ID")
	}
	cursor, n, err := parsePage(req.Cursor, req.Limit)
	if err != nil {
		return nil, err
	}
	query := "SELECT video_id, created_at FROM " + r.table + " WHERE user_id=?"
	args := []any{req.UserId}
	if cursor.id != 0 {
		query += " AND (created_at < ? OR (created_at = ? AND video_id < ?))"
		args = append(args, cursor.created, cursor.created, cursor.id)
	}
	query += " ORDER BY created_at DESC, video_id DESC LIMIT ?"
	rows, err := s.db.QueryContext(ctx, query, append(args, n+1)...)
	if err != nil {
		return nil, databaseError(ctx, err)
	}
	defer rows.Close()
	out := &pb.ListResponse{}
	var last pageCursor
	for rows.Next() {
		var row pageCursor
		if err := rows.Scan(&row.id, &row.created); err != nil {
			return nil, databaseError(ctx, err)
		}
		if len(out.VideoIds) == n {
			out.NextCursor = last.String()
			break
		}
		out.VideoIds = append(out.VideoIds, row.id)
		last = row
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError(ctx, err)
	}
	return out, nil
}

func (s *Server) ListFavorites(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	return s.listSaved(ctx, favorite, req)
}
func (s *Server) ListLikes(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	return s.listSaved(ctx, videoLike, req)
}
