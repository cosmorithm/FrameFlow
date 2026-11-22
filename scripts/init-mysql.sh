#!/bin/sh
# Executed by the official MySQL image on a new data volume.
set -eu
case "$FEED_DB_PASSWORD:$INTERACTION_DB_PASSWORD" in *[!a-f0-9:]*) echo "Expected generated hexadecimal application passwords" >&2; exit 1;; esac
export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"
mysql -uroot <<SQL
CREATE DATABASE frameflow_proto_feed CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE DATABASE frameflow_proto_interaction CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'ff_feed'@'%' IDENTIFIED BY '$FEED_DB_PASSWORD';
CREATE USER 'ff_interaction'@'%' IDENTIFIED BY '$INTERACTION_DB_PASSWORD';
GRANT SELECT, INSERT, UPDATE, DELETE ON frameflow_proto_feed.* TO 'ff_feed'@'%';
GRANT SELECT, INSERT, UPDATE, DELETE ON frameflow_proto_interaction.* TO 'ff_interaction'@'%';
SQL
mysql -uroot frameflow_proto_feed < /schema/feed.sql
mysql -uroot frameflow_proto_interaction < /schema/interaction.sql
mysql -uroot frameflow_proto_feed < /schema/seed.sql
