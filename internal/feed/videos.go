package feed

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	pb "github.com/cosmorithm/FrameFlow/proto/interaction/v1"
	"github.com/gin-gonic/gin"
)

type Video struct {
	ID             uint64    `json:"id"`
	AuthorID       uint64    `json:"author_id"`
	AuthorName     string    `json:"author_name"`
	Caption        string    `json:"caption"`
	VideoURL       string    `json:"video_url"`
	CoverURL       string    `json:"cover_url"`
	Tags           []string  `json:"tags"`
	Views          uint64    `json:"view_count"`
	QualifiedViews uint64    `json:"view_5s_count"`
	Likes          uint64    `json:"like_count"`
	Comments       uint64    `json:"comment_count"`
	Liked          bool      `json:"liked"`
	Favorited      bool      `json:"favorited"`
	Following      bool      `json:"following"`
	Available      bool      `json:"interaction_available"`
	PublishedAt    time.Time `json:"published_at"`
}
type pageCursor struct {
	date time.Time
	id   uint64
}

func pageAfter(t time.Time, id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatUint(id, 10)))
}
func decodeCursor(c *gin.Context) (*pageCursor, bool) {
	v := c.Query("cursor")
	if v == "" {
		return nil, true
	}
	b, err := base64.RawURLEncoding.DecodeString(v)
	s, n, ok := strings.Cut(string(b), "|")
	d, derr := time.Parse(time.RFC3339Nano, s)
	id, ierr := strconv.ParseUint(n, 10, 64)
	if len(v) > 180 || err != nil || !ok || derr != nil || ierr != nil || id == 0 {
		fail(c, 400, "无效的分页游标")
		return nil, false
	}
	return &pageCursor{d.UTC(), id}, true
}
func marks(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }
func argsFor(ids []uint64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}
func mediaURL(key string) string {
	if key == "" {
		return ""
	}
	if strings.HasPrefix(key, "uploads/") {
		return "/" + key
	}
	return "/media/" + key
}
func (s *Server) pageIDs(ctx context.Context, tag string, after *pageCursor, limit int) ([]uint64, error) {
	query := "SELECT v.video_id FROM videos v"
	var args []any
	if tag != "" {
		query += " JOIN video_tags vt ON v.video_id=vt.video_id JOIN tags t ON vt.tag_id=t.tag_id"
	}
	query += " WHERE v.status='published' AND v.published_at IS NOT NULL"
	if tag != "" {
		query += " AND t.name=?"
		args = append(args, tag)
	}
	if after != nil {
		query += " AND (v.published_at < ? OR (v.published_at = ? AND v.video_id < ?))"
		args = append(args, after.date, after.date, after.id)
	}
	query += " ORDER BY v.published_at DESC,v.video_id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]uint64, 0, limit+1)
	for rows.Next() {
		var id uint64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Server) loadVideos(ctx context.Context, ids []uint64) ([]Video, error) {
	videos := make([]Video, 0, len(ids))
	if len(ids) == 0 {
		return videos, nil
	}
	in := marks(len(ids))
	args := argsFor(ids)
	query := "SELECT v.video_id,v.author_id,u.nickname,v.caption,v.video_key,v.cover_key,v.view_count,v.view_5s_count,v.published_at FROM videos v JOIN users u ON v.author_id=u.id WHERE v.status='published' AND v.video_id IN (" + in + ")"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	records := map[uint64]*Video{}
	for rows.Next() {
		var v Video
		var media string
		var cover sql.NullString
		if err = rows.Scan(&v.ID, &v.AuthorID, &v.AuthorName, &v.Caption, &media, &cover, &v.Views, &v.QualifiedViews, &v.PublishedAt); err != nil {
			rows.Close()
			return nil, err
		}
		v.VideoURL = mediaURL(media)
		v.CoverURL = mediaURL(cover.String)
		v.Tags = []string{}
		records[v.ID] = &v
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = s.db.QueryContext(ctx, "SELECT vt.video_id,t.name FROM video_tags vt JOIN tags t ON t.tag_id=vt.tag_id WHERE vt.video_id IN ("+in+") ORDER BY vt.video_id,t.tag_id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint64
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		if v := records[id]; v != nil {
			v.Tags = append(v.Tags, name)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if v := records[id]; v != nil {
			videos = append(videos, *v)
		}
	}
	return videos, nil
}
func (s *Server) enrich(ctx context.Context, viewer uint64, videos []Video) {
	if len(videos) == 0 {
		return
	}
	rpc, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	refs := make([]*pb.VideoRef, len(videos))
	for i, v := range videos {
		refs[i] = &pb.VideoRef{VideoId: v.ID, AuthorId: v.AuthorID}
	}
	res, err := s.interaction.BatchGetInteractions(rpc, &pb.BatchRequest{ViewerId: viewer, Videos: refs})
	if err != nil {
		log.Printf("batch interaction fallback: %v", err)
		return
	}
	mapped := map[uint64]*pb.Interaction{}
	for _, item := range res.Items {
		mapped[item.VideoId] = item
	}
	for i := range videos {
		v := &videos[i]
		if item := mapped[v.ID]; item != nil {
			v.Available = true
			v.Likes = item.LikeCount
			v.Comments = item.CommentCount
			v.Liked = item.Liked
			v.Favorited = item.Favorited
			v.Following = item.Following
		}
	}
}
func (s *Server) feed(c *gin.Context) {
	limit, ok := limitParam(c, 8)
	if !ok {
		return
	}
	after, ok := decodeCursor(c)
	if !ok {
		return
	}
	tag := strings.TrimSpace(c.Query("tag"))
	if utf8.RuneCountInString(tag) > 64 {
		fail(c, 400, "标签过长")
		return
	}
	ctx := c.Request.Context()
	var ids []uint64
	var err error
	cache := s.redis != nil && after == nil && tag == "" && limit == 8
	if cache {
		data, e := s.redis.Get(ctx, pageCacheKey).Bytes()
		if e == nil {
			var stored []uint64
			if json.Unmarshal(data, &stored) == nil && len(stored) <= limit+1 {
				ids = stored
			}
		}
	}
	if ids == nil {
		ids, err = s.pageIDs(ctx, tag, after, limit)
		if err != nil {
			internal(c, err)
			return
		}
		if cache {
			payload, _ := json.Marshal(ids)
			s.redis.Set(ctx, pageCacheKey, payload, 30*time.Second)
		}
	}
	videos, err := s.loadVideos(ctx, ids)
	if err != nil {
		internal(c, err)
		return
	}
	next := ""
	if len(videos) > limit {
		videos = videos[:limit]
		last := videos[limit-1]
		next = pageAfter(last.PublishedAt, last.ID)
	}
	s.enrich(ctx, viewerID(c), videos)
	c.JSON(200, gin.H{"items": videos, "next_cursor": next})
}
func (s *Server) collection(c *gin.Context) {
	user, ok := requireUser(c)
	if !ok {
		return
	}
	n, ok := limitParam(c, 8)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	req := &pb.ListRequest{UserId: user.ID, Limit: uint32(n), Cursor: c.Query("cursor")}
	var res *pb.ListResponse
	var err error
	if c.FullPath() == "/api/favorites" {
		res, err = s.interaction.ListFavorites(ctx, req)
	} else {
		res, err = s.interaction.ListLikes(ctx, req)
	}
	if err != nil {
		rpcError(c, err)
		return
	}
	videos, err := s.loadVideos(c.Request.Context(), res.VideoIds)
	if err != nil {
		internal(c, err)
		return
	}
	s.enrich(c.Request.Context(), user.ID, videos)
	c.JSON(200, gin.H{"items": videos, "next_cursor": res.NextCursor})
}
func (s *Server) names(ctx context.Context, ids []uint64) (map[uint64]string, error) {
	names := map[uint64]string{}
	if len(ids) == 0 {
		return names, nil
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("SELECT id,nickname FROM users WHERE id IN (%s)", marks(len(ids))), argsFor(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint64
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}
