#!/usr/bin/env python3
"""Exercise the running FrameFlow prototype using only the Python standard library.

Run: BASE_URL=http://127.0.0.1:8080 python3 scripts/smoke.py
Use a development database. Every run creates two accounts, two uploaded videos,
one unique tag and a comment thread. These fixtures remain available for inspection.
"""
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import time
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import HTTPCookieProcessor, Request, build_opener

BASE_URL = os.environ.get("BASE_URL", "http://127.0.0.1:8080").rstrip("/")


def check(condition, message):
    if not condition:
        raise AssertionError(message)


class Browser:
    def __init__(self):
        self.http = build_opener(HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(self, path, method="GET", data=None, headers=None, expect=200):
        headers = dict(headers or {})
        if isinstance(data, dict):
            headers["Content-Type"] = "application/json"
            data = json.dumps(data, ensure_ascii=False).encode()
        request = Request(BASE_URL + path, data=data, headers=headers, method=method)
        try:
            response = self.http.open(request, timeout=12)
        except HTTPError as error:
            response = error
        with response:
            body = response.read()
            media_type = response.headers.get_content_type()
            if response.status != expect:
                detail = body[:300].decode("utf-8", "replace")
                # Play tickets and login cookies should not end up in test logs.
                route = path.split("/plays/")[0]
                raise AssertionError(f"{method} {route}: HTTP {response.status}, expected {expect}: {detail}")
        if media_type == "application/json" and body:
            return json.loads(body)
        return body

    def account(self, role, run_id):
        details = {
            "email": f"smoke-{run_id}-{role}@example.test",
            "password": secrets.token_urlsafe(18),
            "nickname": f"验收{role}-{run_id[:6]}",
        }
        user = self.request("/api/register", "POST", details, expect=201)["user"]
        self.request("/api/logout", "POST", expect=204)
        check(self.request("/api/me")["user"] is None, "logout left a valid session")
        self.request("/api/login", "POST", details)
        check(self.request("/api/me")["user"]["id"] == user["id"], "login did not restore the account")
        return user["id"]

    def upload(self, source, tag, caption):
        boundary = "frameflow-" + secrets.token_hex(16)
        parts = []
        for name, value in (("caption", caption), ("tags", tag)):
            parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'.encode())
        parts.append((f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="flower.mp4"'
                      '\r\nContent-Type: video/mp4\r\n\r\n').encode())
        parts.extend((source, f"\r\n--{boundary}--\r\n".encode()))
        return self.request("/api/videos", "POST", b"".join(parts),
                            {"Content-Type": f"multipart/form-data; boundary={boundary}"}, 201)["id"]

    def page(self, tag, limit=8, cursor=None):
        query = {"tag": tag, "limit": limit}
        if cursor:
            query["cursor"] = cursor
        return self.request("/api/feed?" + urlencode(query))

    def video(self, tag, video_id):
        return next(item for item in self.page(tag)["items"] if item["id"] == video_id)

    def set(self, path, enabled):
        reply = self.request(path, "PUT", {"enabled": enabled})
        check(reply["enabled"] is enabled, f"{path} returned an incorrect target state")


def main():
    run_id = secrets.token_hex(8)
    tag = f"smoke-{run_id}"
    source = (Path(__file__).resolve().parents[1] / "media" / "flower.mp4").read_bytes()
    owner, viewer = Browser(), Browser()
    check(owner.request("/api/health")["ok"], "health probe failed")
    owner_id = owner.account("author", run_id)
    viewer_id = viewer.account("viewer", run_id)
    check(owner_id != viewer_id, "accounts not isolated")
    print("PASS  independent email registrations, logout and login")

    owner.request("/api/feed")  # warm the public first page before publishing
    older_id = owner.upload(source, tag, "验收片段 A")
    video_id = owner.upload(source, tag, "验收片段 B")
    video_path = f"/api/videos/{video_id}"
    public_ids = [row["id"] for row in viewer.request("/api/feed")["items"]]
    check(video_id in public_ids, "new publication missing from public feed")
    first = viewer.page(tag, limit=1)
    check([v["id"] for v in first["items"]] == [video_id], "incorrect first tagged page")
    check(first["next_cursor"], "missing next cursor")
    second = viewer.page(tag, limit=1, cursor=first["next_cursor"])
    check([v["id"] for v in second["items"]] == [older_id] and not second["next_cursor"], "cursor skipped or repeated a video")
    initial = viewer.video(tag, video_id)
    check(initial["interaction_available"] and initial["author_id"] == owner_id, "missing gRPC data or incorrect ownership")
    check(tag in initial["tags"], "published tag missing")
    media = viewer.request(initial["video_url"], headers={"Range": "bytes=0-31"}, expect=206)
    check(media == source[:32], "uploaded video byte range did not match the file")
    print("PASS  MP4 publication, media Range, public feed, tag filter and cursor")

    watch = viewer.request(video_path + "/plays", "POST", expect=201)
    five_path = video_path + "/plays/" + watch["play_id"] + "/five-seconds"
    started = time.monotonic()
    viewer.request(five_path, "POST", expect=409)
    viewer.request(video_path + "/plays", "POST", expect=201)
    owner.request(five_path, "POST", expect=400)
    early = viewer.video(tag, video_id)
    check(early["view_count"] == initial["view_count"] + 1 and
          early["view_5s_count"] == initial["view_5s_count"], "start/early-event deduplication failed")

    follow_path = f"/api/users/{owner_id}/follow"
    for target in (video_path + "/like", video_path + "/favorite", follow_path):
        viewer.set(target, True)
        viewer.set(target, True)
    v = viewer.video(tag, video_id)
    check(v["liked"] and v["favorited"] and v["following"], "viewer interaction state missing")
    check(v["like_count"] == initial["like_count"] + 1, "duplicate video likes counted")
    owner_view = owner.video(tag, video_id)
    check(not any(owner_view[key] for key in ("liked", "favorited", "following")), "viewer state leaked to another account")
    for endpoint in ("/api/favorites", "/api/likes"):
        check(video_id in [i["id"] for i in viewer.request(endpoint)["items"]], f"video missing from {endpoint}")
    for target in (video_path + "/like", video_path + "/favorite", follow_path):
        viewer.set(target, False)
        viewer.set(target, False)
    v = viewer.video(tag, video_id)
    check(not any(v[key] for key in ("liked", "favorited", "following")) and
          v["like_count"] == initial["like_count"], "undoing interactions failed")
    print("PASS  idempotent likes, favorites, follows and per-user state")

    comments = video_path + "/comments"
    root = owner.request(comments, "POST", {"content": "原评论"}, expect=201)
    reply = viewer.request(comments, "POST", {"content": "回复", "parent_id": root["id"]}, expect=201)
    nested = owner.request(comments, "POST", {"content": "回复的回复", "parent_id": reply["id"]}, expect=201)
    check(nested["root_id"] == root["id"] and nested["parent_id"] == reply["id"], "incorrect reply thread")
    thread = comments + "?" + urlencode({"root_id": root["id"]})
    reply_like = f"/api/comments/{reply['id']}/like"
    viewer.set(reply_like, True)
    viewer.set(reply_like, True)
    rows = viewer.request(thread)["items"]
    check([i["id"] for i in rows] == [reply["id"], nested["id"]], "incorrect expanded reply order")
    check(rows[0]["liked"] and rows[0]["like_count"] == reply["like_count"] + 1, "duplicate or missing comment like")
    root_view = next(c for c in viewer.request(comments)["items"] if c["id"] == root["id"])
    check(root_view["like_count"] == root["like_count"] and root_view["reply_count"] == 2, "reply like propagated to root")
    check(viewer.video(tag, video_id)["comment_count"] == initial["comment_count"] + 3, "incorrect feed comment count")
    print("PASS  comments, nested reply ancestry, comment counts and independent reply likes")

    time.sleep(max(0, started + 5.15 - time.monotonic()))
    for _ in range(2):
        check(viewer.request(five_path, "POST")["ok"], "five-second event failed")
    final = viewer.video(tag, video_id)
    check(final["view_count"] == initial["view_count"] + 1 and
          final["view_5s_count"] == initial["view_5s_count"] + 1, "duplicate five-second event changed totals")
    print("PASS  five-second gate, replay protection and daily deduplication")
    print(f"Fixtures: tag={tag}, users={owner_id},{viewer_id}, videos={older_id},{video_id}")


if __name__ == "__main__":
    main()
