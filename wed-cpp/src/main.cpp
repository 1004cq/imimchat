#include "http_min.hpp"
#include "store.hpp"

#include <charconv>
#include <cstdlib>
#include <iostream>
#include <string>

namespace {

std::string envOr(const char *name, const char *fallback) {
  const char *v = std::getenv(name);
  return v && v[0] ? v : fallback;
}

uint16_t portOr(const char *name, uint16_t fallback) {
  const char *raw = std::getenv(name);
  if (!raw || !raw[0]) return fallback;
  unsigned int parsed = 0;
  const auto *end = raw + std::char_traits<char>::length(raw);
  if (std::from_chars(raw, end, parsed).ec != std::errc{} || parsed == 0 || parsed > 65535) {
    return fallback;
  }
  return static_cast<uint16_t>(parsed);
}

std::string jsonGet(const std::string &body, const std::string &key) {
  const std::string pat = "\"" + key + "\"";
  auto p = body.find(pat);
  if (p == std::string::npos) return {};
  p = body.find(':', p);
  if (p == std::string::npos) return {};
  p = body.find_first_not_of(" \t\n\r", p + 1);
  if (p == std::string::npos) return {};
  if (body[p] == '"') {
    const auto e = body.find('"', p + 1);
    return e == std::string::npos ? std::string{} : body.substr(p + 1, e - p - 1);
  }
  if (body.compare(p, 4, "null") == 0) return {};
  return {};
}

std::string bearerUserHint(const HttpReq &req) {
  auto it = req.headers.find("authorization");
  if (it == req.headers.end()) return {};
  auto v = it->second;
  while (!v.empty() && v.front() == ' ') v.erase(v.begin());
  const std::string prefix = "Bearer ";
  if (v.size() > prefix.size() && v.compare(0, prefix.size(), prefix) == 0) {
    return v.substr(prefix.size());
  }
  return v;
}

}  // namespace

int main() {
  const auto bind = envOr("WED_BIND", "127.0.0.1");
  const auto port = portOr("WED_PORT", 8088);

  std::unordered_map<std::string, HttpHandler> routes;
  routes["GET /api/health"] = [](const HttpReq &) {
    return HttpRes{200, "application/json; charset=utf-8",
                   R"({"service":"wed-cpp","status":"ok","runtime":"cxx","cutoverReady":false})"};
  };
  routes["GET /api/ready"] = [](const HttpReq &) {
    const auto st = probeStores();
    const bool ok = st.postgres && st.redis;
    std::string body = std::string("{") +
                       "\"postgres\":" + (st.postgres ? "true" : "false") + "," +
                       "\"redis\":" + (st.redis ? "true" : "false") + "," +
                       "\"ok\":" + (ok ? "true" : "false") + "}";
    return HttpRes{ok ? 200 : 503, "application/json; charset=utf-8", body};
  };
  routes["GET /api/migration-status"] = [](const HttpReq &) {
    return HttpRes{200, "application/json; charset=utf-8",
                   R"({"service":"wed-cpp","nodeRuntimeRequired":true,"cutoverReady":false,"next":"auth, private chat, websocket parity"})"};
  };
  routes["POST /api/presence"] = [](const HttpReq &req) {
    const auto token = bearerUserHint(req);
    if (token.empty()) {
      return HttpRes{401, "application/json; charset=utf-8", R"({"error":"unauthorized"})"};
    }
    const auto state = jsonGet(req.body, "state");
    if (state != "foreground" && state != "background" && state != "offline") {
      return HttpRes{400, "application/json; charset=utf-8", R"({"error":"invalid_state"})"};
    }
    // Phase-1: token is required but not verified against Node JWT.
    const std::string uid = token.size() > 24 ? token.substr(0, 24) : token;
    const auto chat = jsonGet(req.body, "activeChatId");
    const bool written = redisSetPresence(uid, state, chat);
    return HttpRes{written ? 200 : 503, "application/json; charset=utf-8",
                   written ? R"({"success":true,"verified":false})" : R"({"error":"redis_unavailable"})"};
  };

  std::clog << "wed-cpp listening " << bind << ":" << port << '\n';
  httpServe(bind, port, routes);
  return 0;
}
