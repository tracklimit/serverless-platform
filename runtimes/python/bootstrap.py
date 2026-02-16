import importlib.util
import json
import os
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler


def load_handler():
    spec = importlib.util.spec_from_file_location("handler", "/var/function/handler.py")
    if spec is None:
        print("ERROR: /var/function/handler.py not found", file=sys.stderr)
        sys.exit(1)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    if not hasattr(module, "handler"):
        print("ERROR: handler.py must define a handler(request) function", file=sys.stderr)
        sys.exit(1)
    return module.handler


handler_fn = load_handler()


class FunctionHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        self._handle()

    def do_GET(self):
        self._handle()

    def _handle(self):
        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length) if content_length > 0 else b""

        request = {
            "method": self.command,
            "path": self.path,
            "headers": dict(self.headers),
            "body": body.decode("utf-8") if body else "",
        }

        try:
            result = handler_fn(request)
            response = json.dumps(result).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(response)
        except Exception as e:
            error = json.dumps({"error": str(e)}).encode("utf-8")
            self.send_response(500)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(error)

    def log_message(self, format, *args):
        print(f"{self.client_address[0]} - {format % args}", file=sys.stderr)


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8080"))
    server = HTTPServer(("0.0.0.0", port), FunctionHandler)
    print(f"Runtime ready on port {port}", file=sys.stderr)
    server.serve_forever()
