package feed

import (
	"context"
	"strconv"
	"strings"
	"time"

	pb "github.com/cosmorithm/FrameFlow/proto/interaction/v1"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func rpcError(c *gin.Context, err error) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		fail(c, 400, "请求参数无效")
	case codes.NotFound:
		fail(c, 404, "互动对象不存在")
	case codes.PermissionDenied:
		fail(c, 403, "无权执行此操作")
	case codes.Unauthenticated:
		fail(c, 401, "请先登录")
	case codes.FailedPrecondition:
		fail(c, 409, "互动对象状态不允许此操作")
	default:
		fail(c, 503, "互动服务暂不可用，请稍后重试")
	}
}
func (s *Server) mutate(c *gin.Context) {
	user, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		fail(c, 400, "需要指定 enabled")
		return
	}
	path := c.FullPath()
	if strings.HasPrefix(path, "/api/videos/") && !s.videoExists(c, id) {
		return
	}
	if path == "/api/users/:id/follow" {
		if user.ID == id {
			fail(c, 400, "不能关注自己")
			return
		}
		exists, err := exists(c.Request.Context(), s.db, "SELECT 1 FROM users WHERE id=?", id)
		if err != nil {
			internal(c, err)
			return
		}
		if !exists {
			fail(c, 404, "用户不存在")
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	m := &pb.MutationRequest{UserId: user.ID, TargetId: id, Enabled: *req.Enabled}
	var res *pb.MutationResponse
	var err error
	switch path {
	case "/api/videos/:id/like":
		res, err = s.interaction.SetLike(ctx, m)
	case "/api/videos/:id/favorite":
		res, err = s.interaction.SetFavorite(ctx, m)
	case "/api/users/:id/follow":
		res, err = s.interaction.SetFollow(ctx, m)
	case "/api/comments/:id/like":
		res, err = s.interaction.SetCommentLike(ctx, m)
	}
	if err != nil {
		rpcError(c, err)
		return
	}
	c.JSON(200, gin.H{"enabled": res.Enabled})
}
func commentJSON(c *pb.Comment, name string) gin.H {
	return gin.H{"id": c.CommentId, "video_id": c.VideoId, "author_id": c.AuthorId, "author_name": name, "parent_id": c.ParentId, "root_id": c.RootId, "content": c.Content, "created_at": c.CreatedAt, "like_count": c.LikeCount, "liked": c.Liked, "reply_count": c.ReplyCount}
}
func (s *Server) comments(c *gin.Context) {
	id, ok := idParam(c)
	if !ok || !s.videoExists(c, id) {
		return
	}
	n, ok := limitParam(c, 20)
	if !ok {
		return
	}
	var root uint64
	var err error
	if raw := c.Query("root_id"); raw != "" {
		root, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			fail(c, 400, "无效的评论 ID")
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	res, err := s.interaction.ListComments(ctx, &pb.ListCommentsRequest{ViewerId: viewerID(c), VideoId: id, RootId: root, Cursor: c.Query("cursor"), Limit: uint32(n)})
	if err != nil {
		rpcError(c, err)
		return
	}
	authors := make([]uint64, len(res.Items))
	for i, v := range res.Items {
		authors[i] = v.AuthorId
	}
	names, err := s.names(c.Request.Context(), authors)
	if err != nil {
		internal(c, err)
		return
	}
	items := make([]gin.H, len(res.Items))
	for i, v := range res.Items {
		items[i] = commentJSON(v, names[v.AuthorId])
	}
	c.JSON(200, gin.H{"items": items, "next_cursor": res.NextCursor})
}
func (s *Server) addComment(c *gin.Context) {
	user, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := idParam(c)
	if !ok || !s.videoExists(c, id) {
		return
	}
	var req struct {
		Content string `json:"content"`
		Parent  uint64 `json:"parent_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Content) == "" {
		fail(c, 400, "请填写评论")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	res, err := s.interaction.CreateComment(ctx, &pb.CreateCommentRequest{UserId: user.ID, VideoId: id, ParentId: req.Parent, Content: strings.TrimSpace(req.Content)})
	if err != nil {
		rpcError(c, err)
		return
	}
	c.JSON(201, commentJSON(res, user.Nickname))
}
