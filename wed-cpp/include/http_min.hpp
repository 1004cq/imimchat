#pragma once
#include <cstdint>
#include <functional>
#include <string>
#include <unordered_map>

struct HttpReq {
  std::string method;
  std::string path;
  std::string body;
  std::unordered_map<std::string, std::string> headers;
};

struct HttpRes {
  int status = 200;
  std::string contentType = "application/json; charset=utf-8";
  std::string body;
};

using HttpHandler = std::function<HttpRes(const HttpReq &)>;

void httpServe(const std::string &bind, uint16_t port,
               const std::unordered_map<std::string, HttpHandler> &routes);
