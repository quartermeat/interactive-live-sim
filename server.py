import json
import os
import queue
import threading
import uuid
import urllib.request
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

ROOT = os.path.dirname(os.path.abspath(__file__))
STATE = {"users": [], "energy": 0, "events": []}
SUBSCRIBERS = []
LOCK = threading.Lock()
CONNECTOR_URL = os.environ.get("CONNECTOR_EVENTS_URL", "http://127.0.0.1:8787/events")

def now():
    return datetime.now(timezone.utc).isoformat()

def snapshot():
    with LOCK:
        return {"users": list(STATE["users"]), "energy": STATE["energy"], "events": list(STATE["events"][-40:])}

def publish(event):
    with LOCK:
        STATE["events"].append(event)
        if event["type"] == "like": STATE["energy"] += 1
        elif event["type"] == "follow": STATE["energy"] += 8
        elif event["type"] == "gift": STATE["energy"] += {"Rose": 5, "Heart Me": 12, "Galaxy": 35, "Dragon": 80}.get(event.get("gift"), 5)
        for subscriber in list(SUBSCRIBERS):
            subscriber.put(event)

def connector_event(event):
    """Translate the normalized connector event into the arena event shape."""
    event_type = event.get("type", "comment")
    username = event.get("user") or "TikTok viewer"
    gift = event.get("gift") or (event.get("text") if event_type == "gift" else None)
    viewer_id = (event.get("raw") or {}).get("user_id") or "tiktok:" + username.lower().replace(" ", "-")
    return {
        "id": str(uuid.uuid4()),
        "at": event.get("receivedAt") or now(),
        "type": event_type,
        "viewer": {"id": viewer_id, "username": username, "avatar_url": (event.get("raw") or {}).get("avatar_url"), "color": "#5de1e6"},
        "comment": event.get("text"),
        "gift": gift,
        "source": event.get("source", "tiktok-connector"),
    }

def connector_loop():
    """Keep the arena subscribed to the normalized TikTok connector."""
    while True:
        try:
            with urllib.request.urlopen(CONNECTOR_URL, timeout=30) as response:
                data = None
                for raw_line in response:
                    line = raw_line.decode("utf-8", errors="replace").strip()
                    if not line:
                        if data:
                            try:
                                connector_payload = json.loads(data)
                                if connector_payload.get("type") in {"comment", "gift", "like", "follow", "join", "live_end"}:
                                    publish(connector_event(connector_payload))
                            except json.JSONDecodeError:
                                pass
                            data = None
                    elif line.startswith("data:"):
                        data = line[5:].strip()
        except Exception:
            threading.Event().wait(2)

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_): pass

    def send_json(self, value, status=200):
        body = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(body)

    def do_OPTIONS(self):
        self.send_response(204)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.send_header("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
        self.end_headers()

    def do_GET(self):
        path = urlparse(self.path).path
        if path == "/api/state": return self.send_json(snapshot())
        if path == "/api/events": return self.stream_events()
        if path == "/" or path == "/control": return self.file("control.html")
        if path == "/stream": return self.file("stream.html")
        if path == "/stream-wasm": return self.file("web/stream-wasm.html")
        if path == "/arena.wasm": return self.binary_file("web/arena.wasm", "application/wasm")
        if path == "/wasm_exec.js": return self.binary_file("web/wasm_exec.js", "text/javascript; charset=utf-8")
        if path == "/emulator": return self.file("index.html")
        return self.file(path.lstrip("/") or "control.html")

    def file(self, name):
        target = os.path.abspath(os.path.join(ROOT, name))
        if not target.startswith(ROOT) or not os.path.isfile(target):
            return self.send_json({"error": "not found"}, 404)
        with open(target, "rb") as handle: body = handle.read()
        content_type = "text/html; charset=utf-8" if target.endswith(".html") else "text/plain; charset=utf-8"
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def binary_file(self, name, content_type):
        target = os.path.abspath(os.path.join(ROOT, name))
        if not target.startswith(ROOT) or not os.path.isfile(target):
            return self.send_json({"error": "not found"}, 404)
        with open(target, "rb") as handle: body = handle.read()
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        path = urlparse(self.path).path
        length = int(self.headers.get("Content-Length", 0))
        try: payload = json.loads(self.rfile.read(length) or b"{}")
        except json.JSONDecodeError: return self.send_json({"error": "invalid json"}, 400)
        if path == "/api/users":
            user = {"id": payload.get("id") or str(uuid.uuid4()), "name": payload.get("name") or "Viewer", "avatar": payload.get("avatar") or "", "color": payload.get("color") or "#7c6cff"}
            with LOCK: STATE["users"].append(user)
            publish({"type": "viewer_joined", "at": now(), "viewer": {"id": user["id"], "username": user["name"], "avatar_url": user["avatar"] or None, "color": user["color"]}})
            return self.send_json(user, 201)
        if path == "/api/reset":
            with LOCK: STATE["users"] = []; STATE["events"] = []; STATE["energy"] = 0
            return self.send_json({"ok": True})
        if path == "/api/events":
            user = payload.get("viewer") or {}
            event = {"id": str(uuid.uuid4()), "at": now(), "type": payload.get("type", "comment"), "viewer": {"id": user.get("id", "simulated"), "username": user.get("username", "Viewer"), "avatar_url": user.get("avatar_url")}, "comment": payload.get("comment"), "gift": payload.get("gift")}
            publish(event)
            return self.send_json(event, 201)
        return self.send_json({"error": "not found"}, 404)

    def stream_events(self):
        channel = queue.Queue()
        with LOCK:
            SUBSCRIBERS.append(channel)
        try:
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Connection", "keep-alive")
            self.send_header("Access-Control-Allow-Origin", "*")
            self.end_headers()
            self.wfile.write(("data: " + json.dumps({"type": "state", **snapshot()}) + "\n\n").encode())
            self.wfile.flush()
            while True:
                event = channel.get()
                self.wfile.write(("data: " + json.dumps(event) + "\n\n").encode())
                self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            pass
        finally:
            with LOCK:
                if channel in SUBSCRIBERS: SUBSCRIBERS.remove(channel)

if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8090"))
    threading.Thread(target=connector_loop, daemon=True).start()
    print(f"Live app running at http://localhost:{port}")
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
