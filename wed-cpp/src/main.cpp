#include <drogon/drogon.h>

#include <charconv>
#include <cstdlib>
#include <string>
#include <string_view>

namespace {

constexpr std::string_view kServiceName{"wed-cpp"};

std::string environmentOr(std::string_view name, std::string_view fallback) {
  const char *value = std::getenv(name.data());
  return value != nullptr && value[0] != '\0' ? value : std::string{fallback};
}

uint16_t portOr(std::string_view name, uint16_t fallback) {
  const char *raw = std::getenv(name.data());
  if (raw == nullptr || raw[0] == '\0') return fallback;

  unsigned int parsed = 0;
  const auto *first = raw;
  const auto *last = raw + std::char_traits<char>::length(raw);
  const auto result = std::from_chars(first, last, parsed);
  if (result.ec != std::errc{} || result.ptr != last || parsed == 0 || parsed > 65535) {
    return fallback;
  }
  return static_cast<uint16_t>(parsed);
}

void addJsonRoutes() {
  drogon::app().registerHandler(
      "/api/health",
      [](const drogon::HttpRequestPtr &,
         std::function<void(const drogon::HttpResponsePtr &)> &&callback) {
        Json::Value response;
        response["service"] = std::string{kServiceName};
        response["status"] = "ok";
        response["runtime"] = "cxx";
        response["migrationPhase"] = "foundation";

        auto httpResponse = drogon::HttpResponse::newHttpJsonResponse(response);
        httpResponse->addHeader("Cache-Control", "no-store");
        callback(httpResponse);
      },
      {drogon::Get});

  drogon::app().registerHandler(
      "/api/migration-status",
      [](const drogon::HttpRequestPtr &,
         std::function<void(const drogon::HttpResponsePtr &)> &&callback) {
        Json::Value response;
        response["service"] = std::string{kServiceName};
        response["nodeRuntimeRequired"] = false;
        response["cutoverReady"] = false;
        response["next"] = "Port authenticated chat, WebSocket protocol, PostgreSQL and Redis adapters before routing production traffic.";

        auto httpResponse = drogon::HttpResponse::newHttpJsonResponse(response);
        httpResponse->addHeader("Cache-Control", "no-store");
        callback(httpResponse);
      },
      {drogon::Get});
}

}  // namespace

int main() {
  const auto bindAddress = environmentOr("WED_BIND", "127.0.0.1");
  const auto port = portOr("WED_PORT", 8081);

  addJsonRoutes();
  drogon::app().addListener(bindAddress, port).setThreadNum(2).run();
  return 0;
}
