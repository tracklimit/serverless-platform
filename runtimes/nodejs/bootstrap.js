const http = require("node:http");

const handlerPath = "/var/function/index.js";
let handlerFn;

try {
  const mod = require(handlerPath);
  handlerFn = mod.handler || mod.default || mod;
  if (typeof handlerFn !== "function") {
    console.error("ERROR: index.js must export a handler function");
    process.exit(1);
  }
} catch (err) {
  console.error(`ERROR: failed to load ${handlerPath}: ${err.message}`);
  process.exit(1);
}

const server = http.createServer(async (req, res) => {
  const chunks = [];
  for await (const chunk of req) {
    chunks.push(chunk);
  }
  const body = Buffer.concat(chunks).toString("utf-8");

  const request = {
    method: req.method,
    path: req.url,
    headers: req.headers,
    body: body,
  };

  try {
    const result = await handlerFn(request);
    const response = JSON.stringify(result);
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(response);
  } catch (err) {
    const error = JSON.stringify({ error: err.message });
    res.writeHead(500, { "Content-Type": "application/json" });
    res.end(error);
  }
});

const port = process.env.PORT || 8080;
server.listen(port, "0.0.0.0", () => {
  console.error(`Runtime ready on port ${port}`);
});
