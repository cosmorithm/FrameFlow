-- Run in a fresh Feed schema after feed.sql. Sample authors cannot log in.
INSERT INTO users (email, password_hash, nickname) VALUES
 ('open-movies@example.invalid','!sample-account','开放电影放映室'),
 ('garden@example.invalid','!sample-account','花园观察员')
ON DUPLICATE KEY UPDATE email = users.email;

INSERT INTO videos (author_id, caption, video_key, cover_key, status, published_at)
SELECT id, '追光的旅人 · Sintel', 'sintel.mp4', 'sintel.jpg', 'published', UTC_TIMESTAMP(3)
FROM users WHERE email='open-movies@example.invalid'
AND NOT EXISTS (SELECT 1 FROM videos WHERE video_key='sintel.mp4');

INSERT INTO videos (author_id, caption, video_key, cover_key, status, published_at)
SELECT id, '草地上的意外来客 · Big Buck Bunny', 'bunny.mp4', 'bunny.jpg', 'published', UTC_TIMESTAMP(3) - INTERVAL 1 HOUR
FROM users WHERE email='open-movies@example.invalid'
AND NOT EXISTS (SELECT 1 FROM videos WHERE video_key='bunny.mp4');

INSERT INTO videos (author_id, caption, video_key, cover_key, status, published_at)
SELECT id, '花瓣之间，风正经过', 'flower.mp4', 'flower.jpg', 'published', UTC_TIMESTAMP(3) - INTERVAL 2 HOUR
FROM users WHERE email='garden@example.invalid'
AND NOT EXISTS (SELECT 1 FROM videos WHERE video_key='flower.mp4');

INSERT INTO tags (name) VALUES ('动画'),('开放电影'),('奇幻'),('自然'),('微距')
ON DUPLICATE KEY UPDATE name = tags.name;
INSERT INTO video_tags (video_id, tag_id)
SELECT v.video_id,t.tag_id FROM videos v JOIN tags t
ON (v.video_key='sintel.mp4' AND t.name IN ('动画','开放电影','奇幻'))
OR (v.video_key='bunny.mp4' AND t.name IN ('动画','开放电影'))
OR (v.video_key='flower.mp4' AND t.name IN ('自然','微距'))
ON DUPLICATE KEY UPDATE tag_id = video_tags.tag_id;
