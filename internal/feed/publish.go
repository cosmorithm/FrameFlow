package feed

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

func (s *Server) publish(c *gin.Context) {
	user, ok := requireUser(c)
	if !ok {
		return
	}
	mediaType, _, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		fail(c, 415, "请用表单上传 MP4 文件")
		return
	}
	const maxFile = 64 << 20
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxFile+(1<<20))
	err = c.Request.ParseMultipartForm(4 << 20)
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			fail(c, 413, "视频文件不能超过 64 MB")
		} else {
			fail(c, 400, "上传表单无效")
		}
		return
	}
	caption := strings.TrimSpace(c.PostForm("caption"))
	if utf8.RuneCountInString(caption) > 1000 {
		fail(c, 400, "文案最多 1000 字")
		return
	}
	tags := []string{}
	seen := map[string]bool{}
	for _, v := range strings.FieldsFunc(c.PostForm("tags"), func(r rune) bool { return r == ',' || r == '，' || r == '#' }) {
		v = strings.TrimSpace(v)
		k := strings.ToLower(v)
		if v == "" || seen[k] {
			continue
		}
		if utf8.RuneCountInString(v) > 64 {
			fail(c, 400, "标签过长")
			return
		}
		seen[k] = true
		tags = append(tags, v)
	}
	if len(tags) > 8 {
		fail(c, 400, "每条视频最多 8 个标签")
		return
	}
	sort.Strings(tags)
	header, err := c.FormFile("file")
	if err != nil {
		fail(c, 400, "缺少视频文件")
		return
	}
	if header.Size > maxFile {
		fail(c, 413, "视频文件不能超过 64 MB")
		return
	}
	src, err := header.Open()
	if err != nil {
		internal(c, err)
		return
	}
	defer src.Close()
	head := make([]byte, 12)
	if _, err = io.ReadFull(src, head); err != nil || string(head[4:8]) != "ftyp" {
		fail(c, 415, "请选择 MP4 视频")
		return
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		internal(c, err)
		return
	}
	name := hex.EncodeToString(token[:]) + ".mp4"
	path := filepath.Join(s.uploadDir, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		internal(c, err)
		return
	}
	keep := false
	defer func() {
		if !keep {
			os.Remove(path)
		}
	}()
	_, err = io.Copy(file, io.MultiReader(bytes.NewReader(head), src))
	closeErr := file.Close()
	if err != nil {
		internal(c, err)
		return
	}
	if closeErr != nil {
		internal(c, closeErr)
		return
	}
	ctx := c.Request.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		internal(c, err)
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "INSERT INTO videos(author_id,caption,video_key,status,published_at) VALUES(?,?,?,'published',UTC_TIMESTAMP(3))", user.ID, caption, "uploads/"+name)
	if err != nil {
		internal(c, err)
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		internal(c, err)
		return
	}
	for _, tag := range tags {
		r, e := tx.ExecContext(ctx, "INSERT INTO tags(name) VALUES(?) ON DUPLICATE KEY UPDATE tag_id=LAST_INSERT_ID(tag_id)", tag)
		if e != nil {
			internal(c, e)
			return
		}
		tagID, e := r.LastInsertId()
		if e != nil {
			internal(c, e)
			return
		}
		_, e = tx.ExecContext(ctx, "INSERT INTO video_tags(video_id,tag_id) VALUES(?,?) ON DUPLICATE KEY UPDATE tag_id=video_tags.tag_id", id, tagID)
		if e != nil {
			internal(c, e)
			return
		}
	}
	// A lost MySQL commit response may still mean the video is visible.
	// Retain the file from this point to keep that record playable.
	keep = true
	if err = tx.Commit(); err != nil {
		internal(c, err)
		return
	}
	if s.redis != nil {
		s.redis.Del(ctx, pageCacheKey)
	}
	c.JSON(201, gin.H{"id": id})
}
