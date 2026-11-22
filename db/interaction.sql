-- Run against the interaction schema (e.g. mysql frameflow_interaction).
CREATE TABLE IF NOT EXISTS video_likes (
  user_id BIGINT UNSIGNED NOT NULL,
  video_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (user_id, video_id),
  KEY idx_like_video (video_id),
  KEY idx_like_user_time (user_id, created_at, video_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS video_favorites (
  user_id BIGINT UNSIGNED NOT NULL,
  video_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (user_id, video_id),
  KEY idx_favorite_user_time (user_id, created_at, video_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS user_follows (
  follower_id BIGINT UNSIGNED NOT NULL,
  followee_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (follower_id, followee_id),
  KEY idx_following_time (follower_id, created_at, followee_id),
  KEY idx_followers_time (followee_id, created_at, follower_id),
  CONSTRAINT chk_follow_other CHECK (follower_id <> followee_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS video_comments (
  comment_id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  video_id BIGINT UNSIGNED NOT NULL,
  author_id BIGINT UNSIGNED NOT NULL,
  parent_id BIGINT UNSIGNED DEFAULT NULL,
  root_id BIGINT UNSIGNED DEFAULT NULL,
  content TEXT NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) DEFAULT NULL,
  PRIMARY KEY (comment_id),
  KEY idx_comment_thread_time (video_id, root_id, created_at, comment_id),
  CONSTRAINT chk_comment_level CHECK ((parent_id IS NULL) = (root_id IS NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS comment_likes (
  user_id BIGINT UNSIGNED NOT NULL,
  comment_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (user_id, comment_id),
  KEY idx_comment_like_target (comment_id),
  CONSTRAINT fk_comment_like_target FOREIGN KEY (comment_id) REFERENCES video_comments(comment_id)
) ENGINE=InnoDB;
